package llmpipeline

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dbdsl/internal/llm"
	"dbdsl/internal/validate"
)

func TestRunPlanWithMockGeneratesValidBundle(t *testing.T) {
	dir := t.TempDir()
	taskPath := filepath.Join(dir, "TASK.md")
	if err := os.WriteFile(taskPath, []byte("Products are stored in a catalog.\n"), 0o600); err != nil {
		t.Fatalf("write task: %v", err)
	}
	outDir := filepath.Join(dir, "out")
	result, err := RunPlan(context.Background(), llm.NewDefaultMockClient(), PlanOptions{
		TaskPath: taskPath,
		OutDir:   outDir,
		Model:    "mock-model",
	})
	if err != nil {
		t.Fatalf("RunPlan failed: %v", err)
	}
	if !result.ValidationReport.OK() {
		t.Fatalf("expected validation ok, got %v", result.ValidationReport.Errors)
	}
	if !result.GeneratedDBML {
		t.Fatalf("expected generated DBML")
	}
	for _, name := range []string{
		"TASK.md",
		"source_units.yaml",
		"requirement_atoms.yaml",
		"functional_decomposition.yaml",
		"crud_matrix.yaml",
		"review_decisions.yaml",
		"db_model.dsl.yaml",
		"requirement_atoms.proposed.json",
		"model_plan.proposed.json",
		"dbdsl_patch.proposed.json",
		"validation_report.json",
		"lint_report.json",
		"model.dbml",
		"traceability_report.md",
	} {
		if _, err := os.Stat(filepath.Join(outDir, name)); err != nil {
			t.Fatalf("expected %s: %v", name, err)
		}
	}
	if validation := validate.ValidateFile(filepath.Join(outDir, "db_model.dsl.yaml")); !validation.OK() {
		t.Fatalf("written model does not validate: %v", validation.Errors)
	}
	requestLog, err := os.ReadFile(filepath.Join(outDir, "llm_runs", "001_requirement_extraction", "request.json"))
	if err != nil {
		t.Fatalf("read request log: %v", err)
	}
	for _, needle := range []string{"Authorization", "OPENAI_API_KEY", "sk-"} {
		if strings.Contains(string(requestLog), needle) {
			t.Fatalf("request log contains secret-like value %q: %s", needle, string(requestLog))
		}
	}
}

func TestRunPlanRejectsUnknownSourceUnitBeforeYAMLRewrite(t *testing.T) {
	dir := t.TempDir()
	taskPath := filepath.Join(dir, "TASK.md")
	if err := os.WriteFile(taskPath, []byte("Products are stored in a catalog.\n"), 0o600); err != nil {
		t.Fatalf("write task: %v", err)
	}
	badExtraction := json.RawMessage(`{
	  "requirement_atoms": [
	    {
	      "id": "LLM-RA-001",
	      "statement": "Bad source ref.",
	      "atom_type": "entity",
	      "modeling_relevance": "direct_db",
	      "source_units": ["RAW-SU-999"],
	      "functional_area": "catalog",
	      "functional_pattern": "catalog_management",
	      "support_level": "explicit",
	      "confidence": "high",
	      "requires_review": false,
	      "modeling_outcome": "represented"
	    }
	  ],
	  "functional_areas": [],
	  "actors": [],
	  "operations": [],
	  "review_candidates": [],
	  "warnings": [],
	  "confidence_summary": {}
	}`)
	mock := llm.NewDefaultMockClient()
	mock.Structured["requirement_extraction"] = badExtraction
	_, err := RunPlan(context.Background(), mock, PlanOptions{
		TaskPath: taskPath,
		OutDir:   filepath.Join(dir, "out"),
		Model:    "mock-model",
	})
	if err == nil || !strings.Contains(err.Error(), "unknown source unit RAW-SU-999") {
		t.Fatalf("expected unknown source unit error, got %v", err)
	}
}

func TestRunPlanNormalizesConstraintFieldFromSourceField(t *testing.T) {
	dir := t.TempDir()
	taskPath := filepath.Join(dir, "TASK.md")
	if err := os.WriteFile(taskPath, []byte("Each product has a name.\n"), 0o600); err != nil {
		t.Fatalf("write task: %v", err)
	}
	patch := json.RawMessage(`{
	  "operations": [
	    {
	      "operation": "add_entity",
	      "entity": {
	        "id": "Product",
	        "label": "Product",
	        "description": "Persistent product.",
	        "table_name": "products",
	        "kind": "regular",
	        "evidence": {
	          "source_units": ["RAW-SU-001"],
	          "requirement_atoms": ["LLM-RA-001"],
	          "review_decisions": [],
	          "support_level": "explicit",
	          "confidence": "high",
	          "notes": []
	        },
	        "attributes": [
	          {
	            "id": "product_name",
	            "label": "Name",
	            "description": "Product name.",
	            "type": "string",
	            "required": false,
	            "precision": null,
	            "scale": null,
	            "default": null,
	            "source_field": "name",
	            "enum_values": [],
	            "notes": [],
	            "evidence": {
	              "source_units": ["RAW-SU-001"],
	              "requirement_atoms": ["LLM-RA-001"],
	              "review_decisions": [],
	              "support_level": "explicit",
	              "confidence": "high",
	              "notes": []
	            }
	          }
	        ]
	      },
	      "relationship": null,
	      "constraint": null,
	      "state_machine": null,
	      "derived_view": null,
	      "file_spec": null,
	      "import_spec": null
	    },
	    {
	      "operation": "add_constraint",
	      "entity": null,
	      "relationship": null,
	      "constraint": {
	        "id": "product_name_required",
	        "type": "required",
	        "owner": "Product",
	        "field": "name",
	        "fields": [],
	        "value": null,
	        "min": null,
	        "max": null,
	        "pattern": "",
	        "expression": "",
	        "description": "Product name is required.",
	        "evidence": {
	          "source_units": ["RAW-SU-001"],
	          "requirement_atoms": ["LLM-RA-001"],
	          "review_decisions": [],
	          "support_level": "explicit",
	          "confidence": "high",
	          "notes": []
	        }
	      },
	      "state_machine": null,
	      "derived_view": null,
	      "file_spec": null,
	      "import_spec": null
	    }
	  ],
	  "warnings": [],
	  "unresolved_questions": [],
	  "confidence_summary": {"overall": "test", "risks": ""}
	}`)
	mock := llm.NewDefaultMockClient()
	mock.Structured["dbdsl_patch"] = patch
	outDir := filepath.Join(dir, "out")
	result, err := RunPlan(context.Background(), mock, PlanOptions{
		TaskPath: taskPath,
		OutDir:   outDir,
		Model:    "mock-model",
	})
	if err != nil {
		t.Fatalf("RunPlan failed: %v", err)
	}
	if !result.ValidationReport.OK() {
		t.Fatalf("expected validation ok, got %v", result.ValidationReport.Errors)
	}
	model, err := os.ReadFile(filepath.Join(outDir, "db_model.dsl.yaml"))
	if err != nil {
		t.Fatalf("read model: %v", err)
	}
	if !strings.Contains(string(model), "field: product_name") {
		t.Fatalf("expected normalized constraint field, got:\n%s", string(model))
	}
	report, err := os.ReadFile(filepath.Join(outDir, "validation_report.json"))
	if err != nil {
		t.Fatalf("read validation report: %v", err)
	}
	if !strings.Contains(string(report), "normalized constraint product_name_required field from name to product_name") {
		t.Fatalf("expected normalization warning, got:\n%s", string(report))
	}
}

func TestRunPlanDropsGeneratedIDAttributeForRegularEntities(t *testing.T) {
	dir := t.TempDir()
	taskPath := filepath.Join(dir, "TASK.md")
	if err := os.WriteFile(taskPath, []byte("Each product has a name.\n"), 0o600); err != nil {
		t.Fatalf("write task: %v", err)
	}
	patch := json.RawMessage(`{
	  "operations": [
	    {
	      "operation": "add_entity",
	      "entity": {
	        "id": "ENT-PRODUCT",
	        "label": "Product",
	        "description": "Persistent product.",
	        "table_name": "products",
	        "kind": "regular",
	        "evidence": {
	          "source_units": ["RAW-SU-001"],
	          "requirement_atoms": ["LLM-RA-001"],
	          "review_decisions": [],
	          "support_level": "explicit",
	          "confidence": "high",
	          "notes": []
	        },
	        "attributes": [
	          {
	            "id": "id",
	            "label": "Product ID",
	            "description": "Technical product identifier.",
	            "type": "id",
	            "required": false,
	            "precision": null,
	            "scale": null,
	            "default": null,
	            "source_field": "",
	            "enum_values": [],
	            "notes": [],
	            "evidence": {
	              "source_units": ["RAW-SU-001"],
	              "requirement_atoms": ["LLM-RA-001"],
	              "review_decisions": [],
	              "support_level": "explicit",
	              "confidence": "medium",
	              "notes": []
	            }
	          },
	          {
	            "id": "product_id",
	            "label": "Product ID",
	            "description": "Stable implementation identifier.",
	            "type": "id",
	            "required": true,
	            "precision": null,
	            "scale": null,
	            "default": null,
	            "source_field": "",
	            "enum_values": [],
	            "notes": [],
	            "evidence": {
	              "source_units": ["RAW-SU-001"],
	              "requirement_atoms": ["LLM-RA-001"],
	              "review_decisions": [],
	              "support_level": "explicit",
	              "confidence": "medium",
	              "notes": []
	            }
	          },
	          {
	            "id": "ATTR-PRODUCT-NAME",
	            "label": "Name",
	            "description": "Product name.",
	            "type": "string",
	            "required": false,
	            "precision": null,
	            "scale": null,
	            "default": null,
	            "source_field": "name",
	            "enum_values": [],
	            "notes": [],
	            "evidence": {
	              "source_units": ["RAW-SU-001"],
	              "requirement_atoms": ["LLM-RA-001"],
	              "review_decisions": [],
	              "support_level": "explicit",
	              "confidence": "high",
	              "notes": []
	            }
	          }
	        ]
	      },
	      "relationship": null,
	      "constraint": null,
	      "state_machine": null,
	      "derived_view": null,
	      "file_spec": null,
	      "import_spec": null
	    },
	    {
	      "operation": "add_constraint",
	      "entity": null,
	      "relationship": null,
	      "constraint": {
	        "id": "product_name_required",
	        "type": "required",
	        "owner": "ENT-PRODUCT",
	        "field": "ATTR-PRODUCT-NAME",
	        "fields": [],
	        "value": null,
	        "min": null,
	        "max": null,
	        "pattern": "",
	        "expression": "",
	        "description": "Product name is required.",
	        "evidence": {
	          "source_units": ["RAW-SU-001"],
	          "requirement_atoms": ["LLM-RA-001"],
	          "review_decisions": [],
	          "support_level": "explicit",
	          "confidence": "high",
	          "notes": []
	        }
	      },
	      "state_machine": null,
	      "derived_view": null,
	      "file_spec": null,
	      "import_spec": null
	    }
	  ],
	  "warnings": [],
	  "unresolved_questions": [],
	  "confidence_summary": {"overall": "test", "risks": ""}
	}`)
	mock := llm.NewDefaultMockClient()
	mock.Structured["dbdsl_patch"] = patch
	outDir := filepath.Join(dir, "out")
	result, err := RunPlan(context.Background(), mock, PlanOptions{
		TaskPath: taskPath,
		OutDir:   outDir,
		Model:    "mock-model",
	})
	if err != nil {
		t.Fatalf("RunPlan failed: %v", err)
	}
	if !result.ValidationReport.OK() {
		t.Fatalf("expected validation ok, got %v", result.ValidationReport.Errors)
	}
	model, err := os.ReadFile(filepath.Join(outDir, "db_model.dsl.yaml"))
	if err != nil {
		t.Fatalf("read model: %v", err)
	}
	if strings.Contains(string(model), "\n      - id: id\n") {
		t.Fatalf("expected generated id attribute to be dropped, got:\n%s", string(model))
	}
	if strings.Contains(string(model), "\n        - id: product_id\n") {
		t.Fatalf("expected inferred product_id attribute to be dropped, got:\n%s", string(model))
	}
	if !strings.Contains(string(model), "\n        - id: name\n") {
		t.Fatalf("expected invalid attribute id to normalize to name, got:\n%s", string(model))
	}
	dbml, err := os.ReadFile(filepath.Join(outDir, "model.dbml"))
	if err != nil {
		t.Fatalf("read DBML: %v", err)
	}
	if count := strings.Count(string(dbml), "\n  id int"); count != 1 {
		t.Fatalf("expected one generated id column, got %d:\n%s", count, string(dbml))
	}
	if strings.Contains(string(dbml), "`ATTR-PRODUCT-NAME`") || !strings.Contains(string(dbml), "\n  name varchar(255) [not null]") {
		t.Fatalf("expected normalized DBML column name, got:\n%s", string(dbml))
	}
	report, err := os.ReadFile(filepath.Join(outDir, "validation_report.json"))
	if err != nil {
		t.Fatalf("read validation report: %v", err)
	}
	if !strings.Contains(string(report), "dropped generated primary key attribute ENT-PRODUCT.id") {
		t.Fatalf("expected dropped id warning, got:\n%s", string(report))
	}
	if !strings.Contains(string(report), "dropped generated primary key attribute ENT-PRODUCT.product_id") {
		t.Fatalf("expected dropped product_id warning, got:\n%s", string(report))
	}
	if !strings.Contains(string(report), "normalized attribute ENT-PRODUCT.ATTR-PRODUCT-NAME id to name") {
		t.Fatalf("expected attribute id normalization warning, got:\n%s", string(report))
	}
	if !strings.Contains(string(report), "set attribute ENT-PRODUCT.name required=true from required constraint product_name_required") {
		t.Fatalf("expected required propagation warning, got:\n%s", string(report))
	}
}
