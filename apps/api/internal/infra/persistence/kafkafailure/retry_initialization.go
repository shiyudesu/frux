package infrakafkafailure

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	infrakafka "github.com/shiyudesu/frux/internal/infra/kafka"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	retryInitializationStateInitializing = "initializing"
	retryInitializationStateComplete     = "complete"
)

type RetryOffsetInitializationStore struct {
	db *gorm.DB
}

type retryOffsetInitializationLease struct {
	session  *gorm.DB
	conn     *sql.Conn
	identity infrakafka.RetryOffsetInitializationIdentity
	once     sync.Once
	closeErr error
}

func NewRetryOffsetInitializationStore(
	db *gorm.DB,
) *RetryOffsetInitializationStore {
	return &RetryOffsetInitializationStore{db: db}
}

func (s *RetryOffsetInitializationStore) Lock(
	ctx context.Context,
	identity infrakafka.RetryOffsetInitializationIdentity,
) (infrakafka.RetryOffsetInitializationLease, error) {
	if s == nil || s.db == nil || identity.Fingerprint() == "" {
		return nil, errors.New("retry offset initialization store is unavailable")
	}
	sqlDB, err := s.db.DB()
	if err != nil {
		return nil, err
	}
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return nil, err
	}
	lockKey := identity.Fingerprint()
	if _, err := conn.ExecContext(
		ctx,
		"SELECT pg_advisory_lock(hashtextextended($1, 3))",
		lockKey,
	); err != nil {
		_ = conn.Close()
		return nil, err
	}
	session := s.db.Session(&gorm.Session{NewDB: true, Initialized: true})
	session.Statement.ConnPool = conn
	return &retryOffsetInitializationLease{
		session: session, conn: conn, identity: identity,
	}, nil
}

func (l *retryOffsetInitializationLease) Load(
	ctx context.Context,
) (infrakafka.RetryOffsetInitializationState, error) {
	if l == nil || l.session == nil {
		return infrakafka.RetryOffsetInitializationState{}, errors.New(
			"retry offset initialization lease is unavailable",
		)
	}
	return l.load(ctx, l.session)
}

func (l *retryOffsetInitializationLease) Ensure(
	ctx context.Context,
	partitions []infrakafka.RetryOffsetInitializationPartition,
) (infrakafka.RetryOffsetInitializationState, error) {
	if l == nil || l.session == nil || len(partitions) == 0 {
		return infrakafka.RetryOffsetInitializationState{}, errors.New(
			"retry offset initialization plan is empty",
		)
	}
	err := l.session.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		marker, err := l.lockMarker(tx)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			marker = RetryGroupInitializationModel{
				Identity:      l.identity.Fingerprint(),
				Environment:   l.identity.Environment,
				TopicPrefix:   l.identity.TopicPrefix,
				ConsumerGroup: l.identity.ConsumerGroup,
				Topic:         l.identity.Topic,
				Version:       l.identity.Version,
				State:         retryInitializationStateInitializing,
			}
			if err := tx.Create(&marker).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if !l.markerMatches(marker) {
			return errors.New("retry offset initialization identity collision")
		}

		sorted := append(
			[]infrakafka.RetryOffsetInitializationPartition(nil),
			partitions...,
		)
		sort.Slice(sorted, func(left, right int) bool {
			return sorted[left].Partition < sorted[right].Partition
		})
		added := false
		for _, partition := range sorted {
			if partition.Partition < 0 || partition.InitialOffset < 0 {
				return errors.New("invalid retry offset initialization partition")
			}
			var stored RetryGroupInitializationPartitionModel
			err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where(
					"identity = ? AND partition = ?",
					l.identity.Fingerprint(),
					partition.Partition,
				).
				Take(&stored).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				stored = RetryGroupInitializationPartitionModel{
					Identity:      l.identity.Fingerprint(),
					Partition:     partition.Partition,
					InitialOffset: partition.InitialOffset,
					Committed:     partition.Committed,
				}
				if partition.Committed {
					now := time.Now().UTC()
					stored.CommittedAt = &now
				}
				if err := tx.Create(&stored).Error; err != nil {
					return err
				}
				added = true
				continue
			}
			if err != nil {
				return err
			}
			if stored.InitialOffset != partition.InitialOffset {
				return errors.New("retry offset initialization plan changed")
			}
			if partition.Committed && !stored.Committed {
				now := time.Now().UTC()
				if err := tx.Model(&RetryGroupInitializationPartitionModel{}).
					Where(
						"identity = ? AND partition = ? AND committed = FALSE",
						l.identity.Fingerprint(),
						partition.Partition,
					).
					Updates(map[string]any{
						"committed":    true,
						"committed_at": now,
						"updated_at":   now,
					}).Error; err != nil {
					return err
				}
			}
		}
		if added && marker.State == retryInitializationStateComplete {
			if err := tx.Model(&RetryGroupInitializationModel{}).
				Where("identity = ?", l.identity.Fingerprint()).
				Updates(map[string]any{
					"state":        retryInitializationStateInitializing,
					"completed_at": nil,
					"updated_at":   time.Now().UTC(),
				}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return infrakafka.RetryOffsetInitializationState{}, err
	}
	return l.Load(ctx)
}

func (l *retryOffsetInitializationLease) MarkCommitted(
	ctx context.Context,
	partition int32,
) error {
	if l == nil || l.session == nil || partition < 0 {
		return errors.New("invalid retry offset initialization partition")
	}
	return l.session.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&RetryGroupInitializationPartitionModel{}).
			Where(
				"identity = ? AND partition = ?",
				l.identity.Fingerprint(),
				partition,
			).
			Updates(map[string]any{
				"committed":    true,
				"committed_at": time.Now().UTC(),
				"updated_at":   time.Now().UTC(),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("retry offset initialization partition is missing")
		}
		return nil
	})
}

func (l *retryOffsetInitializationLease) Complete(ctx context.Context) error {
	if l == nil || l.session == nil {
		return errors.New("retry offset initialization lease is unavailable")
	}
	return l.session.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		marker, err := l.lockMarker(tx)
		if err != nil {
			return err
		}
		if !l.markerMatches(marker) {
			return errors.New("retry offset initialization identity collision")
		}
		var total int64
		if err := tx.Model(&RetryGroupInitializationPartitionModel{}).
			Where("identity = ?", l.identity.Fingerprint()).
			Count(&total).Error; err != nil {
			return err
		}
		var pending int64
		if err := tx.Model(&RetryGroupInitializationPartitionModel{}).
			Where(
				"identity = ? AND committed = FALSE",
				l.identity.Fingerprint(),
			).
			Count(&pending).Error; err != nil {
			return err
		}
		if total == 0 || pending != 0 {
			return errors.New("retry offset initialization is incomplete")
		}
		now := time.Now().UTC()
		return tx.Model(&RetryGroupInitializationModel{}).
			Where("identity = ?", l.identity.Fingerprint()).
			Updates(map[string]any{
				"state":        retryInitializationStateComplete,
				"completed_at": now,
				"updated_at":   now,
			}).Error
	})
}

func (l *retryOffsetInitializationLease) Close() error {
	if l == nil {
		return nil
	}
	l.once.Do(func() {
		if l.conn == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, unlockErr := l.conn.ExecContext(
			ctx,
			"SELECT pg_advisory_unlock(hashtextextended($1, 3))",
			l.identity.Fingerprint(),
		)
		closeErr := l.conn.Close()
		l.closeErr = errors.Join(unlockErr, closeErr)
	})
	return l.closeErr
}

func (l *retryOffsetInitializationLease) load(
	ctx context.Context,
	db *gorm.DB,
) (infrakafka.RetryOffsetInitializationState, error) {
	var marker RetryGroupInitializationModel
	err := db.WithContext(ctx).
		Where("identity = ?", l.identity.Fingerprint()).
		Take(&marker).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return infrakafka.RetryOffsetInitializationState{
			Partitions: make(
				map[int32]infrakafka.RetryOffsetInitializationPartition,
			),
		}, nil
	}
	if err != nil {
		return infrakafka.RetryOffsetInitializationState{}, err
	}
	if !l.markerMatches(marker) {
		return infrakafka.RetryOffsetInitializationState{}, errors.New(
			"retry offset initialization identity collision",
		)
	}
	if marker.State != retryInitializationStateInitializing &&
		marker.State != retryInitializationStateComplete {
		return infrakafka.RetryOffsetInitializationState{}, errors.New(
			"invalid retry offset initialization state",
		)
	}
	var stored []RetryGroupInitializationPartitionModel
	if err := db.WithContext(ctx).
		Where("identity = ?", l.identity.Fingerprint()).
		Order("partition ASC").
		Find(&stored).Error; err != nil {
		return infrakafka.RetryOffsetInitializationState{}, err
	}
	partitions := make(
		map[int32]infrakafka.RetryOffsetInitializationPartition,
		len(stored),
	)
	for _, partition := range stored {
		partitions[partition.Partition] = infrakafka.RetryOffsetInitializationPartition{
			Partition: partition.Partition, InitialOffset: partition.InitialOffset,
			Committed: partition.Committed,
		}
	}
	return infrakafka.RetryOffsetInitializationState{
		Exists:     true,
		Complete:   marker.State == retryInitializationStateComplete,
		Partitions: partitions,
	}, nil
}

func (l *retryOffsetInitializationLease) lockMarker(
	tx *gorm.DB,
) (RetryGroupInitializationModel, error) {
	var marker RetryGroupInitializationModel
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("identity = ?", l.identity.Fingerprint()).
		Take(&marker).Error
	return marker, err
}

func (l *retryOffsetInitializationLease) markerMatches(
	marker RetryGroupInitializationModel,
) bool {
	return marker.Identity == l.identity.Fingerprint() &&
		marker.Environment == l.identity.Environment &&
		marker.TopicPrefix == l.identity.TopicPrefix &&
		marker.ConsumerGroup == l.identity.ConsumerGroup &&
		marker.Topic == l.identity.Topic &&
		marker.Version == l.identity.Version
}

func (l *retryOffsetInitializationLease) String() string {
	if l == nil {
		return ""
	}
	return fmt.Sprintf(
		"%s/%s",
		l.identity.ConsumerGroup,
		l.identity.Topic,
	)
}

var _ infrakafka.RetryOffsetInitializationStore = (*RetryOffsetInitializationStore)(nil)
var _ infrakafka.RetryOffsetInitializationLease = (*retryOffsetInitializationLease)(nil)
