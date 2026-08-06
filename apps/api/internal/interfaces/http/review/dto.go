package interfaceshttpreview

type machineSignalRequest struct {
	Label        string   `json:"label"`
	Confidence   float64  `json:"confidence"`
	EvidenceRefs []string `json:"evidence_refs"`
}

type machineResultRequest struct {
	VideoID       int64                  `json:"video_id"`
	ReviewVersion int                    `json:"review_version"`
	Provider      string                 `json:"provider"`
	ModelVersion  string                 `json:"model_version"`
	PolicyVersion int                    `json:"policy_version"`
	Signals       []machineSignalRequest `json:"signals"`
}

type machineResultResponse struct {
	CaseID        int64  `json:"case_id"`
	Status        string `json:"status"`
	Outcome       string `json:"outcome"`
	PolicyVersion int    `json:"policy_version"`
	Duplicate     bool   `json:"duplicate"`
}
