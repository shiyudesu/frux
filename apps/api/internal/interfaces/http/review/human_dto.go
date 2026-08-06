package interfaceshttpreview

import (
	"strings"

	domainreview "github.com/shiyudesu/frux/internal/domain/review"
)

func humanCaseResponseFromDomain(reviewCase *domainreview.ReviewCase) humanCaseResponse {
	if reviewCase == nil {
		return humanCaseResponse{}
	}
	return humanCaseResponse{
		ID: reviewCase.ID, VideoID: reviewCase.VideoID, ReviewVersion: reviewCase.ReviewVersion,
		Status: reviewCase.Status, PolicyVersion: reviewCase.PolicyVersion, Priority: reviewCase.Priority,
		Version: reviewCase.Version, AssignedReviewerID: reviewCase.AssignedReviewerID,
		LeaseExpiresAt: reviewCase.LeaseExpiresAt, CreatedAt: reviewCase.CreatedAt,
		UpdatedAt: reviewCase.UpdatedAt, ClosedAt: reviewCase.ClosedAt,
	}
}

func humanCaseDetailResponseFromDomain(detail *domainreview.HumanCaseDetail) humanCaseDetailResponse {
	response := humanCaseDetailResponse{
		Case: humanCaseResponseFromDomain(detail.Case),
		Subject: humanSubjectResponse{
			VideoID: detail.Subject.VideoID, AuthorID: detail.Subject.AuthorID,
			Title: detail.Subject.Title, Description: detail.Subject.Description,
			MediaURL: detail.Subject.MediaURL, CoverURL: detail.Subject.CoverURL,
			ReviewVersion: detail.Subject.ReviewVersion,
		},
		History: humanHistoryResponse{
			Signals: []evidenceSignalResponse{}, AutomatedDecisions: []automatedDecisionResponse{},
			Assignments: []assignmentResponse{}, HumanDecisions: []humanDecisionResponse{},
		},
	}
	for _, signal := range detail.History.Signals {
		response.History.Signals = append(response.History.Signals, evidenceSignalResponse{
			ID: signal.ID, ResultID: signal.ResultID, Label: signal.Label, Confidence: signal.Confidence,
			EvidenceRefs: signal.EvidenceRefs, Provider: signal.Provider, ModelVersion: signal.ModelVersion,
			PolicyVersion: signal.PolicyVersion, SourceKind: evidenceSourceKind(signal.Provider),
			CreatedAt: signal.CreatedAt,
		})
	}

	for _, decision := range detail.History.AutomatedDecisions {
		response.History.AutomatedDecisions = append(response.History.AutomatedDecisions, automatedDecisionResponse{
			ID: decision.ID, ResultID: decision.ResultID, Outcome: decision.Outcome,
			PolicyVersion: decision.PolicyVersion, CreatedAt: decision.CreatedAt,
		})
	}
	for _, assignment := range detail.History.Assignments {
		response.History.Assignments = append(response.History.Assignments, assignmentResponse{
			ID: assignment.ID, ReviewerID: assignment.ReviewerID, Event: assignment.Event,
			CaseVersion: assignment.CaseVersion, LeaseUntil: assignment.LeaseUntil,
			CreatedAt: assignment.CreatedAt,
		})
	}
	for _, decision := range detail.History.HumanDecisions {
		response.History.HumanDecisions = append(
			response.History.HumanDecisions, humanDecisionResponseFromDomain(decision),
		)
	}
	return response
}

func evidenceSourceKind(provider string) string {
	if strings.EqualFold(strings.TrimSpace(provider), "manual-seed") {
		return "test_seed"
	}
	return "unverified"
}

func humanDecisionResponseFromDomain(decision *domainreview.HumanDecision) humanDecisionResponse {
	if decision == nil {
		return humanDecisionResponse{}
	}
	return humanDecisionResponse{
		ID: decision.ID, ReviewerID: decision.ReviewerID, Outcome: decision.Outcome,
		ReasonCode: decision.ReasonCode, Note: decision.Note, ReviewVersion: decision.ReviewVersion,
		CaseVersion: decision.CaseVersion, CreatedAt: decision.CreatedAt,
	}
}
