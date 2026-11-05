package serve

import (
	"testing"

	names "github.com/doze-dev/doze-names"
)

func TestOthersListsWhatOtherDozeProgramsRoute(t *testing.T) {
	home := t.TempDir()
	aws := names.Open(home, "doze-aws")
	lease, err := aws.Claim(names.Qualified("aws", "harbour"))
	if err != nil {
		t.Skipf("no names zone here: %v", err)
	}
	if err := lease.Route("127.0.0.17:4566"); err != nil {
		t.Fatal(err)
	}
	// A stack's own name is not listed, nor an unrouted one.
	mine := names.Open(home, "doze")
	if _, err := mine.Claim(names.Qualified("db", "shop")); err != nil {
		t.Fatal(err)
	}
	got := others(home)
	if len(got) != 1 || got[0].Name != "aws.harbour.doze" || got[0].Owner != "doze-aws" || got[0].URL == "" {
		t.Fatalf("others = %+v", got)
	}
}
