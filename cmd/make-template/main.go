// Command make-template rebuilds internal/bundle/template.pro from exported song bundles.
// The first bundle gives the Blank cue and the lyric slide style; every bundle adds its
// background video and its library group links.
//
//	go run ./cmd/make-template internal/bundle/template.pro "samples/Nimesi sun.probundle" "samples/Suuruutes Äärellä.probundle"
package main

import (
	"fmt"
	"os"

	"google.golang.org/protobuf/proto"

	"probundle/internal/bundle"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: make-template OUT.pro PRIMARY.probundle [MORE.probundle...]")
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "make-template:", err)
		os.Exit(1)
	}
}

func run(out string, inputs []string) error {
	var bundles []*bundle.Bundle
	for _, in := range inputs {
		b, err := bundle.Read(in)
		if err != nil {
			return fmt.Errorf("%s: %w", in, err)
		}
		bundles = append(bundles, b)
	}
	p, err := bundle.MakeTemplate(bundles[0], bundles[1:]...)
	if err != nil {
		return err
	}
	data, err := proto.MarshalOptions{Deterministic: true}.Marshal(p)
	if err != nil {
		return err
	}
	return os.WriteFile(out, data, 0o644)
}
