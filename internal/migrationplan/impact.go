package migrationplan

func buildImpactSummary(plan *Plan) {
	if plan == nil {
		return
	}

	summary := PlanSummary{
		TotalOperations: len(plan.Operations),
	}

	for _, operation := range plan.Operations {
		switch operation.RiskLevel {
		case RiskLow:
			summary.LowRiskOperations++
		case RiskMedium:
			summary.MediumRiskOperations++
		case RiskHigh:
			summary.HighRiskOperations++
		case RiskCritical:
			summary.CriticalRiskOperations++
		}

		if operation.RequiresReview {
			summary.OperationsRequiringReview++
		}

		switch operation.RollbackAssessment {
		case RollbackSchemaOnly:
			summary.SchemaOnlyRollbackOperations++
		case RollbackManual:
			summary.ManualRollbackOperations++
		}
	}

	plan.Summary = summary
}
