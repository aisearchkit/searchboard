package logengine

import "testing"

func TestMatch(t *testing.T) {
	got := Match("java.lang.OutOfMemoryError: Java heap space")
	if len(got) != 1 || got[0].Name != "jvm-oom" {
		t.Fatalf("unexpected: %+v", got)
	}
	if len(Match("all good")) != 0 {
		t.Fatal("false positive")
	}
}
