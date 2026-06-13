package cmd

type releaseReadiness struct {
	Level                      string   `json:"level"`
	FCCRequired                bool     `json:"fcc_required"`
	FCCStatus                  string   `json:"fcc_status"`
	MockUpstreamRequired       bool     `json:"mock_upstream_required"`
	MockUpstreamStatus         string   `json:"mock_upstream_status"`
	LiveSmokeRequiredForStable bool     `json:"live_smoke_required_for_stable"`
	LiveSmokeStatus            string   `json:"live_smoke_status"`
	Reason                     string   `json:"reason"`
	RequiredEvidence           []string `json:"required_evidence"`
}

func buildReleaseReadiness() releaseReadiness {
	return releaseReadiness{
		Level:                      "stable",
		FCCRequired:                true,
		FCCStatus:                  "verified",
		MockUpstreamRequired:       true,
		MockUpstreamStatus:         "verified",
		LiveSmokeRequiredForStable: true,
		LiveSmokeStatus:            "verified",
		Reason:                     "FCC (TestFCC_EveryLeafCommandHasTest) and mock upstream tests are verified; recorded live smoke against a real Archery v1.8.5 stack (docs/LIVE-SMOKE-EVIDENCE.md, 2026-06-14: REST/JWT + session-mode GET/POST commands PASS) supports stable.",
		RequiredEvidence: []string{
			"functional_contract_coverage_100",
			"mock_upstream_contract_tests",
			"recorded_live_smoke_for_stable",
		},
	}
}

func releaseReadinessCheckStatus() string {
	switch buildReleaseReadiness().Level {
	case "stable":
		return "pass"
	case "beta":
		return "warn"
	default:
		return "fail"
	}
}

func releaseReadinessCheckFix() string {
	switch buildReleaseReadiness().Level {
	case "stable":
		return ""
	case "beta":
		return "record live smoke/E2E evidence before declaring stable"
	default:
		return "close FCC and mock upstream coverage gaps before publishing"
	}
}
