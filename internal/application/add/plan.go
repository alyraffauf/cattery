package add

import (
	"sort"

	"github.com/alyraffauf/cattery/internal/deployment"
)

// BuildPlan freezes the preflighted items into a BatchPlan. Items are sorted
// by target path for display while execution proceeds in that same order
// (PLAN.md Section 11.6: sources and baselines are written sequentially in
// target-path order).
func BuildPlan(items []ItemPlanInput) (BatchPlan, error) {
	plans, err := buildItems(items)
	if err != nil {
		return BatchPlan{}, err
	}
	sorted := sortByTarget(plans)
	return NewBatchPlan(BatchPlanInput{Items: sorted, ExecutionOrder: sequentialOrder(len(sorted))})
}

// DryRun renders a plan as target-sorted planned records with no secret or
// baseline effects. Rendering the ADD/ADD-SECRET lines is the CLI's job.
func DryRun(plan BatchPlan) Result {
	records := plannedRecords(plan.Items())
	return Result{Items: records, Summary: Summary{Planned: len(records)}}
}

// buildItems validates each candidate and freezes it into an ItemPlan.
func buildItems(items []ItemPlanInput) ([]ItemPlan, error) {
	plans := make([]ItemPlan, 0, len(items))
	for _, candidate := range items {
		item, err := NewItemPlan(candidate)
		if err != nil {
			return nil, err
		}
		plans = append(plans, item)
	}
	return plans, nil
}

// sortByTarget returns a defensive copy of plans ordered by target path.
func sortByTarget(plans []ItemPlan) []ItemPlan {
	sorted := append([]ItemPlan(nil), plans...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].TargetRelativePath() < sorted[j].TargetRelativePath()
	})
	return sorted
}

// sequentialOrder returns [0, 1, ..., count-1] for in-order execution.
func sequentialOrder(count int) []int {
	order := make([]int, count)
	for index := range order {
		order[index] = index
	}
	return order
}

// plannedRecords maps each item to a planned result record.
func plannedRecords(items []ItemPlan) []ItemResult {
	records := make([]ItemResult, 0, len(items))
	for _, item := range items {
		records = append(records, plannedRecord(item))
	}
	return records
}

// plannedRecord renders one item as a StatusPlanned record.
func plannedRecord(item ItemPlan) ItemResult {
	return itemRecord(item, StatusPlanned)
}

func itemRecord(item ItemPlan, status ItemStatus) ItemResult {
	return ItemResult{
		Target: item.TargetRelativePath(),
		Source: item.SourceRepositoryPath(),
		Status: status,
		Secret: item.Kind() == deployment.FileSecret,
	}
}
