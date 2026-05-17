package main

import (
	"context"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/providers/social"
)

const command = "social/youtube_collect"

func main() {
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in social.Input) error {
	var out social.Output
	err := skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
		var collectErr error
		out, collectErr = social.Collect(ctx, social.DefaultClient(), social.PlatformYouTube, in)
		return collectErr
	})
	if err != nil {
		return err
	}
	return skillout.EmitWithCAS(ctx, rc, command, out)
}
