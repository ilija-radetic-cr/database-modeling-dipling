package generate

import "dbdsl/internal/dsl"

func constraintFields(constraint dsl.Constraint) []string {
	fields := append([]string(nil), constraint.Fields...)
	if constraint.Field != "" && !stringInSlice(fields, constraint.Field) {
		fields = append(fields, constraint.Field)
	}
	return fields
}

func constraintBoundValue(constraint dsl.Constraint) any {
	if constraint.Value != nil {
		return constraint.Value
	}
	if constraint.Min != nil {
		return constraint.Min
	}
	if constraint.Max != nil {
		return constraint.Max
	}
	return nil
}

func stringInSlice(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
