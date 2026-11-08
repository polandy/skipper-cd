package deploy

import "testing"

func TestMaskEnvDiff_HidesValuesKeepsKeys(t *testing.T) {
	diff := "diff --git a/s/app.env b/s/app.env\n" +
		"index 1111111..2222222 100644\n" +
		"--- a/s/app.env\n" +
		"+++ b/s/app.env\n" +
		"@@ -1,3 +1,3 @@ HEADER_SECRET=ctx\n" +
		" # rotated monthly\n" +
		"-API_TOKEN=old-secret\n" +
		"+API_TOKEN=new-secret\n" +
		" DB_URL=postgres://u:p@db/x?a=b\n" +
		"\\ No newline at end of file\n"

	want := "diff --git a/s/app.env b/s/app.env\n" +
		"index 1111111..2222222 100644\n" +
		"--- a/s/app.env\n" +
		"+++ b/s/app.env\n" +
		"@@ -1,3 +1,3 @@\n" +
		" # rotated monthly\n" +
		"-API_TOKEN=***\n" +
		"+API_TOKEN=***\n" +
		" DB_URL=***\n" +
		"\\ No newline at end of file\n"

	if got := maskEnvDiff(diff); got != want {
		t.Errorf("maskEnvDiff:\n got %q\nwant %q", got, want)
	}
}

// Inside a hunk a removed "-- k=v" line reads "--- k=v"; it is content, not a
// file header, and must be masked.
func TestMaskEnvDiff_MasksHeaderLookalikeContentLines(t *testing.T) {
	diff := "--- a/x.sql\n+++ b/x.sql\n@@ -1,2 +1,2 @@\n--- key=old\n+++ key=new\n"
	want := "--- a/x.sql\n+++ b/x.sql\n@@ -1,2 +1,2 @@\n--- key=***\n+++ key=***\n"

	if got := maskEnvDiff(diff); got != want {
		t.Errorf("maskEnvDiff:\n got %q\nwant %q", got, want)
	}
}
