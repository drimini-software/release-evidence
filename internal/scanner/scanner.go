package scanner

import (
	"context"
	"sort"
	"time"

	"github.com/drimini-software/release-evidence/internal/detector"
	"github.com/drimini-software/release-evidence/internal/model"
	"github.com/drimini-software/release-evidence/internal/packet"
	"github.com/drimini-software/release-evidence/internal/repository"
)

const RuleSetVersion = "1"

type Options struct {
	Repository  repository.Options
	ToolVersion string
	Now         func() time.Time
}

func Scan(ctx context.Context, root string, options Options) (model.Packet, error) {
	if options.ToolVersion == "" {
		options.ToolVersion = "dev"
	}
	if options.Now == nil {
		options.Now = time.Now
	}

	snapshot, err := repository.Inspect(ctx, root, options.Repository)
	if err != nil {
		return model.Packet{}, err
	}

	exclusions := append([]string{}, snapshot.Exclusions...)
	sort.Strings(exclusions)
	diagnostics := append([]model.Diagnostic{}, snapshot.Diagnostics...)
	findings := detector.Evaluate(snapshot)

	result := model.Packet{
		SchemaVersion: model.SchemaVersion,
		Tool: model.Tool{
			Name:           "release-evidence",
			Version:        options.ToolVersion,
			RuleSetVersion: RuleSetVersion,
		},
		Repository: model.Repository{
			Name:             snapshot.Name,
			Revision:         snapshot.Revision,
			RevisionState:    snapshot.RevisionState,
			WorkingTreeState: snapshot.WorkingTreeState,
			WorkingTreeNote:  snapshot.WorkingTreeNote,
		},
		Boundary: model.Boundary{
			Root:       ".",
			Exclusions: exclusions,
			Limits:     snapshot.Limits,
		},
		Scan: model.Scan{
			GeneratedAt:     options.Now().UTC().Format(time.RFC3339Nano),
			Complete:        snapshot.Complete,
			EntriesVisited:  snapshot.EntriesVisited,
			FilesConsidered: len(snapshot.Paths),
		},
		Findings:    findings,
		Diagnostics: diagnostics,
		Assessment:  model.Assessment{Status: "not_started"},
		Decision:    model.Decision{Status: "not_made"},
	}

	if err := packet.SetIntegrity(&result); err != nil {
		return model.Packet{}, err
	}
	if err := model.ValidatePacket(result); err != nil {
		return model.Packet{}, err
	}
	return result, nil
}
