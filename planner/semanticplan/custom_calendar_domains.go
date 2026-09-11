package semanticplan

// CustomCalendarDomainCount returns the number of node-owned custom calendar
// domains represented by a typed semantic DAG.
func CustomCalendarDomainCount(nodes []SemanticPlanNode) int {
	count := 0
	for _, node := range nodes {
		switch node := node.(type) {
		case TimeOffsetNode:
			if node.CustomCalendar != nil {
				count++
			}
		case CumulativeWindowNode:
			if node.CustomCalendarRolling != nil {
				count++
			}
			if node.CustomCalendarGrainToDate != nil {
				count++
			}
		}
	}
	return count
}

// CustomCalendarDomainDatasets returns node-owned custom-calendar datasets in
// their deterministic first-seen order.
func CustomCalendarDomainDatasets(nodes []SemanticPlanNode) []string {
	out := make([]string, 0, len(nodes))
	seen := make(map[string]struct{}, len(nodes))
	add := func(name string) {
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	for _, node := range nodes {
		switch node := node.(type) {
		case TimeOffsetNode:
			if node.CustomCalendar != nil {
				add(node.CustomCalendar.Dataset.Name)
			}
		case CumulativeWindowNode:
			if node.CustomCalendarRolling != nil {
				add(node.CustomCalendarRolling.Dataset.Name)
			}
			if node.CustomCalendarGrainToDate != nil {
				add(node.CustomCalendarGrainToDate.Dataset.Name)
			}
		}
	}
	return out
}
