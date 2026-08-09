package backup

import "sort"

type StackBackupSummary struct {
	StackName     string      `json:"stack_name"`
	StackExists   bool        `json:"stack_exists"`
	RunCount      int         `json:"run_count"`
	LatestRun     *RunSummary `json:"latest_run,omitempty"`
	RepoSizeBytes uint64      `json:"repo_size_bytes,omitempty"`
}

type Overview struct {
	Configured bool                 `json:"configured"`
	Stacks     []StackBackupSummary `json:"stacks"`
}

func mergeStackNames(withStacks, withRuns []string) []string {
	seen := map[string]bool{}
	for _, name := range withStacks {
		seen[name] = true
	}
	for _, name := range withRuns {
		seen[name] = true
	}

	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func stackWorthListing(exists bool, runCount int) bool {
	return exists || runCount > 0
}

func summariseStack(name string, exists bool, runs []RunSummary) StackBackupSummary {
	summary := StackBackupSummary{
		StackName:   name,
		StackExists: exists,
		RunCount:    len(runs),
	}
	if len(runs) > 0 {
		latest := runs[0]
		summary.LatestRun = &latest
		summary.RepoSizeBytes = latest.RepoSizeBytes
	}
	return summary
}

func (s *Service) BuildOverview() (*Overview, error) {
	stacks, err := s.stacks.ListStacks()
	if err != nil {
		return nil, err
	}
	existing := make(map[string]bool, len(stacks))
	names := make([]string, 0, len(stacks))
	for _, stack := range stacks {
		existing[stack.Name] = true
		names = append(names, stack.Name)
	}

	withRuns, err := s.persistence.ListStacksWithRuns()
	if err != nil {
		return nil, err
	}

	overview := &Overview{Configured: s.Configured()}
	for _, name := range mergeStackNames(names, withRuns) {
		runs, err := s.persistence.RunSummaries(name)
		if err != nil {
			return nil, err
		}
		if !stackWorthListing(existing[name], len(runs)) {
			continue
		}
		overview.Stacks = append(overview.Stacks, summariseStack(name, existing[name], runs))
	}
	if overview.Stacks == nil {
		overview.Stacks = []StackBackupSummary{}
	}
	return overview, nil
}
