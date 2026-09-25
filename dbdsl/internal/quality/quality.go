package quality

import (
	"fmt"
	"strings"

	"dbdsl/internal/lint"
	"dbdsl/internal/validate"
)

type Summary struct {
	ValidationErrors   int    `json:"validation_errors"`
	LintWarnings       int    `json:"lint_warnings"`
	LintInfo           int    `json:"lint_info"`
	BlockingIssues     int    `json:"blocking_issues"`
	TraceabilityStatus string `json:"traceability_status"`
	DBMLStatus         string `json:"dbml_status"`
}

type Issue struct {
	ID        string `json:"id"`
	Severity  string `json:"severity"`
	Code      string `json:"code"`
	ElementID string `json:"element_id,omitempty"`
	Message   string `json:"message"`
	Blocking  bool   `json:"blocking"`
	Accepted  bool   `json:"accepted"`
}

type Report struct {
	Summary Summary `json:"summary"`
	Issues  []Issue `json:"issues"`
}

func BuildReport(modelPath string, dbmlReady bool) Report {
	validation := validate.ValidateFile(modelPath)
	lintResult := lint.LintFile(modelPath)

	report := Report{
		Summary: Summary{
			TraceabilityStatus: "complete",
			DBMLStatus:         "not_generated",
		},
	}
	if dbmlReady {
		report.Summary.DBMLStatus = "ready"
	}

	for i, err := range validation.Errors {
		report.Issues = append(report.Issues, Issue{
			ID:       fmt.Sprintf("VAL-%03d", i+1),
			Severity: "error",
			Code:     "validation_failed",
			Message:  err,
			Blocking: true,
		})
	}
	for i, issue := range lintResult.Issues {
		blocking := issue.Severity == lint.SeverityError
		report.Issues = append(report.Issues, Issue{
			ID:        fmt.Sprintf("LINT-%03d", i+1),
			Severity:  issue.Severity,
			Code:      issue.Code,
			ElementID: normalizeElementID(issue.Element),
			Message:   issue.Message,
			Blocking:  blocking,
		})
	}

	for _, issue := range report.Issues {
		switch issue.Severity {
		case "error":
			if strings.HasPrefix(issue.ID, "VAL-") {
				report.Summary.ValidationErrors++
			}
			report.Summary.BlockingIssues++
		case "warning":
			report.Summary.LintWarnings++
		case "info":
			report.Summary.LintInfo++
		}
	}
	if report.Summary.ValidationErrors > 0 {
		report.Summary.TraceabilityStatus = "incomplete"
		report.Summary.DBMLStatus = "blocked"
	}
	return report
}

func normalizeElementID(element string) string {
	parts := strings.Fields(element)
	if len(parts) < 2 {
		return element
	}
	switch parts[0] {
	case "entity":
		return "table:" + parts[1]
	case "attribute":
		return "field:" + parts[1]
	case "relationship":
		return "relationship:" + parts[1]
	case "derived_view":
		return "derived_view:" + parts[1]
	case "constraint":
		return "constraint:" + parts[1]
	case "file_spec":
		return "file_spec:" + parts[1]
	case "import_spec":
		return "import_spec:" + parts[1]
	case "state_machine":
		return "state_machine:" + parts[1]
	default:
		return element
	}
}
