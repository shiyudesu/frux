package infrakafkafailure_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	applicationeventstream "github.com/shiyudesu/frux/internal/application/eventstream"
	infraconfig "github.com/shiyudesu/frux/internal/infra/config"
	infrakafka "github.com/shiyudesu/frux/internal/infra/kafka"
	infrakafkafailure "github.com/shiyudesu/frux/internal/infra/persistence/kafkafailure"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type retryInitializationHandler struct{}

func (retryInitializationHandler) Handle(
	context.Context,
	applicationeventstream.Event,
) (applicationeventstream.Outcome, error) {
	return applicationeventstream.OutcomeDurableSuccess, nil
}

func TestRetryOffsetInitializationDurableAcrossPostgresAndKafka(t *testing.T) {
	brokersValue := strings.TrimSpace(os.Getenv("FRUX_KAFKA_TEST_BROKERS"))
	dsn := strings.TrimSpace(os.Getenv("FRUX_POSTGRES_TEST_DSN"))
	if brokersValue == "" || dsn == "" {
		t.Skip("FRUX_KAFKA_TEST_BROKERS and FRUX_POSTGRES_TEST_DSN are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	db := newRetryInitializationPostgresDB(t, dsn)
	if err := db.AutoMigrate(
		&infrakafkafailure.RetryGroupInitializationModel{},
		&infrakafkafailure.RetryGroupInitializationPartitionModel{},
	); err != nil {
		t.Fatalf("migrate retry initialization markers: %v", err)
	}
	store := infrakafkafailure.NewRetryOffsetInitializationStore(db)
	prefix := fmt.Sprintf("retrydurableitest%d", time.Now().UnixNano())
	cfg := retryInitializationKafkaConfig(
		strings.Split(brokersValue, ","),
		prefix,
	)
	backbone, err := infrakafka.Start(ctx, cfg, nil, nil)
	if err != nil {
		t.Fatalf("start Kafka backbone: %v", err)
	}
	t.Cleanup(func() {
		_ = backbone.Close(context.Background())
		cleanupRetryInitializationKafka(t, cfg)
	})
	adminClient, err := kgo.NewClient(kgo.SeedBrokers(cfg.Brokers...))
	if err != nil {
		t.Fatal(err)
	}
	defer adminClient.Close()
	admin := kadm.NewClient(adminClient)

	t.Run("first initialization restart and retention loss", func(t *testing.T) {
		topic, group := retryInitializationNames(
			t,
			cfg,
			infrakafka.GroupEmbeddingVideoPublishedActive,
			1,
		)
		first := newRetryInitializationConsumer(
			t, ctx, cfg, store, infrakafka.GroupEmbeddingVideoPublishedActive, 1,
		)
		closeRetryInitializationConsumer(t, first)
		assertRetryInitializationMarker(t, db, cfg, group, topic, true)
		starts, err := admin.ListStartOffsets(ctx, topic)
		if err != nil || starts.Error() != nil {
			t.Fatal(errors.Join(err, starts.Error()))
		}
		assertRetryOffsetsEqualStarts(t, ctx, admin, group, topic, starts)

		second := newRetryInitializationConsumer(
			t, ctx, cfg, store, infrakafka.GroupEmbeddingVideoPublishedActive, 1,
		)
		closeRetryInitializationConsumer(t, second)
		assertRetryOffsetsEqualStarts(t, ctx, admin, group, topic, starts)

		produced, err := adminClient.ProduceSync(ctx, &kgo.Record{
			Topic: topic, Key: []byte("video:retention-loss"),
			Value: []byte(`{"retained":true}`),
		}).First()
		if err != nil {
			t.Fatal(err)
		}
		target := make(kadm.Offsets)
		target.Add(kadm.Offset{
			Topic: topic, Partition: produced.Partition, At: produced.Offset,
			Metadata: "forced-retention-loss",
		})
		responses, err := admin.CommitOffsets(ctx, group, target)
		if err != nil || responses.Error() != nil {
			t.Fatal(errors.Join(err, responses.Error()))
		}
		deletions := make(kadm.Offsets)
		deletions.Add(kadm.Offset{
			Topic: topic, Partition: produced.Partition, At: produced.Offset + 1,
		})
		deleted, err := admin.DeleteRecords(ctx, deletions)
		if err != nil || deleted.Error() != nil {
			t.Fatal(errors.Join(err, deleted.Error()))
		}
		_, err = infrakafka.NewRetryTierConsumer(
			ctx,
			cfg,
			infrakafka.GroupEmbeddingVideoPublishedActive,
			1,
			retryInitializationHandler{},
			nil,
			infrakafka.WithRetryOffsetInitializationStore(store),
		)
		if !errors.Is(err, infrakafka.ErrConsumerDataLoss) {
			t.Fatalf("retention loss error=%v", err)
		}
	})

	t.Run("deleted established offset fails explicitly", func(t *testing.T) {
		topic, group := retryInitializationNames(
			t,
			cfg,
			infrakafka.GroupEmbeddingVideoPublishedActive,
			2,
		)
		consumer := newRetryInitializationConsumer(
			t, ctx, cfg, store, infrakafka.GroupEmbeddingVideoPublishedActive, 2,
		)
		closeRetryInitializationConsumer(t, consumer)
		var deletedOffsets kadm.TopicsSet
		deletedOffsets.Add(topic, 0)
		deleted, err := admin.DeleteOffsets(ctx, group, deletedOffsets)
		if err != nil || deleted.Error() != nil {
			t.Fatal(errors.Join(err, deleted.Error()))
		}
		_, err = infrakafka.NewRetryTierConsumer(
			ctx,
			cfg,
			infrakafka.GroupEmbeddingVideoPublishedActive,
			2,
			retryInitializationHandler{},
			nil,
			infrakafka.WithRetryOffsetInitializationStore(store),
		)
		if !errors.Is(err, infrakafka.ErrConsumerDataLoss) {
			t.Fatalf("deleted offset error=%v", err)
		}
		assertRetryInitializationMarker(t, db, cfg, group, topic, true)
	})

	t.Run("concurrent replicas share one durable initialization", func(t *testing.T) {
		topic, group := retryInitializationNames(
			t,
			cfg,
			infrakafka.GroupEmbeddingVideoPublishedActive,
			3,
		)
		start := make(chan struct{})
		errs := make(chan error, 2)
		var wait sync.WaitGroup
		for range 2 {
			wait.Add(1)
			go func() {
				defer wait.Done()
				<-start
				consumer, err := infrakafka.NewRetryTierConsumer(
					ctx,
					cfg,
					infrakafka.GroupEmbeddingVideoPublishedActive,
					3,
					retryInitializationHandler{},
					nil,
					infrakafka.WithRetryOffsetInitializationStore(store),
				)
				if err == nil {
					closeContext, closeCancel := context.WithCancel(
						context.Background(),
					)
					closeCancel()
					err = consumer.Run(closeContext)
				}
				errs <- err
			}()
		}
		close(start)
		wait.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		assertRetryInitializationMarker(t, db, cfg, group, topic, true)
	})
}

func newRetryInitializationConsumer(
	t *testing.T,
	ctx context.Context,
	cfg infraconfig.KafkaConfig,
	store infrakafka.RetryOffsetInitializationStore,
	group infrakafka.ConsumerGroupID,
	tier int,
) *infrakafka.Consumer {
	t.Helper()
	consumer, err := infrakafka.NewRetryTierConsumer(
		ctx,
		cfg,
		group,
		tier,
		retryInitializationHandler{},
		nil,
		infrakafka.WithRetryOffsetInitializationStore(store),
	)
	if err != nil {
		t.Fatal(err)
	}
	return consumer
}

func closeRetryInitializationConsumer(t *testing.T, consumer *infrakafka.Consumer) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := consumer.Run(ctx); err != nil {
		t.Fatal(err)
	}
}

func retryInitializationNames(
	t *testing.T,
	cfg infraconfig.KafkaConfig,
	group infrakafka.ConsumerGroupID,
	tier int,
) (string, string) {
	t.Helper()
	recovery, err := infrakafka.Recovery(group)
	if err != nil {
		t.Fatal(err)
	}
	tierSpec, found := recovery.RetryTier(tier)
	if !found {
		t.Fatalf("retry tier %d is not registered", tier)
	}
	topic, err := infrakafka.TopicName(cfg.TopicPrefix, tierSpec.Topic)
	if err != nil {
		t.Fatal(err)
	}
	resolvedGroup, err := infrakafka.RecoveryConsumerGroupName(
		cfg.TopicPrefix,
		group,
		tier,
	)
	if err != nil {
		t.Fatal(err)
	}
	return topic, resolvedGroup
}

func assertRetryInitializationMarker(
	t *testing.T,
	db *gorm.DB,
	cfg infraconfig.KafkaConfig,
	group string,
	topic string,
	complete bool,
) {
	t.Helper()
	identity, err := infrakafka.NewRetryOffsetInitializationIdentity(
		cfg.Environment,
		cfg.TopicPrefix,
		group,
		topic,
	)
	if err != nil {
		t.Fatal(err)
	}
	var marker infrakafkafailure.RetryGroupInitializationModel
	if err := db.Where("identity = ?", identity.Fingerprint()).
		Take(&marker).Error; err != nil {
		t.Fatal(err)
	}
	if (marker.State == "complete") != complete ||
		marker.ConsumerGroup != group || marker.Topic != topic {
		t.Fatalf("marker=%+v", marker)
	}
	var partitions int64
	if err := db.Model(&infrakafkafailure.RetryGroupInitializationPartitionModel{}).
		Where("identity = ? AND committed = TRUE", identity.Fingerprint()).
		Count(&partitions).Error; err != nil {
		t.Fatal(err)
	}
	if partitions == 0 {
		t.Fatal("durable marker has no committed partitions")
	}
}

func assertRetryOffsetsEqualStarts(
	t *testing.T,
	ctx context.Context,
	admin *kadm.Client,
	group string,
	topic string,
	starts kadm.ListedOffsets,
) {
	t.Helper()
	committed, err := admin.FetchOffsetsForTopics(ctx, group, topic)
	if err != nil || committed.Error() != nil {
		t.Fatal(errors.Join(err, committed.Error()))
	}
	for partition, start := range starts[topic] {
		offset, found := committed.Lookup(topic, partition)
		if !found || offset.At != start.Offset {
			t.Fatalf(
				"partition=%d committed=%+v start=%+v",
				partition,
				offset,
				start,
			)
		}
	}
}

func retryInitializationKafkaConfig(
	brokers []string,
	prefix string,
) infraconfig.KafkaConfig {
	return infraconfig.KafkaConfig{
		Enabled: true, Environment: "test", Brokers: brokers,
		ClientID: "frux-retry-initialization-integration", TopicPrefix: prefix,
		AllowLocalProvisioning: true,
		Authentication: infraconfig.KafkaAuthenticationConfig{
			Mechanism: "none",
		},
		Timeouts: infraconfig.KafkaTimeoutConfig{
			Dial: "5s", Request: "10s", Produce: "10s",
			Admin: "10s", Shutdown: "10s",
		},
		Consumer: infraconfig.KafkaConsumerConfig{
			MaxPollRecords: 10, MaxPollBytes: 1 << 20,
			PartitionConcurrency: 2, DrainTimeout: "5s",
		},
		ProductionValidation: infraconfig.KafkaProductionValidationConfig{
			ReplicationFactor: 1, MinInSyncReplicas: 1,
		},
	}
}

func cleanupRetryInitializationKafka(
	t *testing.T,
	cfg infraconfig.KafkaConfig,
) {
	t.Helper()
	client, err := kgo.NewClient(kgo.SeedBrokers(cfg.Brokers...))
	if err != nil {
		t.Errorf("create cleanup Kafka client: %v", err)
		return
	}
	defer client.Close()
	names := make([]string, 0)
	for _, topic := range infrakafka.Topics() {
		name, err := infrakafka.TopicName(cfg.TopicPrefix, topic.ID)
		if err == nil {
			names = append(names, name)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, _ = kadm.NewClient(client).DeleteTopics(ctx, names...)
}

func newRetryInitializationPostgresDB(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("frux_retry_init_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Errorf("drop schema: %v", err)
		}
		_ = admin.Close()
	})
	sqlDB, err := sql.Open("pgx", retryInitializationDSNWithSchema(dsn, schema))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(
		gormpostgres.New(gormpostgres.Config{Conn: sqlDB}),
		&gorm.Config{TranslateError: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func retryInitializationDSNWithSchema(dsn string, schema string) string {
	if strings.Contains(dsn, "://") {
		parsed, err := url.Parse(dsn)
		if err == nil {
			query := parsed.Query()
			query.Set("search_path", schema)
			query.Set("TimeZone", "UTC")
			parsed.RawQuery = query.Encode()
			return parsed.String()
		}
	}
	return strings.TrimSpace(dsn) + " search_path=" + schema + " TimeZone=UTC"
}
