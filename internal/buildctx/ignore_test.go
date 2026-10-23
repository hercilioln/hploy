package buildctx

import "testing"

func TestIgnoreMatching(t *testing.T) {
	// The real .dockerignore shape of a PHP/Node app.
	ig := ParseIgnore(`
# comments and blank lines are skipped

.git
node_modules
vendor
storage
*.log
**/.DS_Store
.env
!.env.example
`)

	excluded := []string{
		".git", ".git/config", ".git/objects/ab/cdef",
		"node_modules", "node_modules/left-pad/index.js",
		"vendor/autoload.php",
		// This is the one that shipped 700 KB of screenshots: the directory
		// pattern has to reach the files under it.
		"storage", "storage/app/public/print.png",
		"debug.log",
		".DS_Store", "public/img/.DS_Store",
		".env",
	}
	for _, p := range excluded {
		if !ig.Excluded(p) {
			t.Errorf("%q should be excluded", p)
		}
	}

	kept := []string{
		"Dockerfile", "composer.json", "app/Http/Kernel.php",
		"public/index.php", "resources/js/app.js",
		".env.example", // the "!" exception
		"storage.php",  // not the storage directory
		"src/logger.php",
		// A Docker gotcha worth pinning: "*.log" is rooted, so a log in a
		// subdirectory survives. "**/*.log" is what catches those.
		"logs/app.log",
	}
	for _, p := range kept {
		if ig.Excluded(p) {
			t.Errorf("%q should be kept", p)
		}
	}
}

func TestLastRuleWins(t *testing.T) {
	ig := ParseIgnore("!keep.txt\nkeep.txt\n")
	if !ig.Excluded("keep.txt") {
		t.Error("the later rule should win")
	}
	ig = ParseIgnore("keep.txt\n!keep.txt\n")
	if ig.Excluded("keep.txt") {
		t.Error("the later exception should win")
	}
}

func TestGlobs(t *testing.T) {
	cases := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"*.log", "a.log", true},
		{"*.log", "dir/a.log", false}, // "*" does not cross a slash
		{"**/*.log", "dir/a.log", true},
		{"**/*.log", "a.log", true}, // "**/" may match nothing
		{"temp?", "temp1", true},
		{"temp?", "temp12", false},
		{"a/b", "a/b/c", true}, // a matched directory takes its contents
		{"docs", "mydocs", false},
		{"/leading", "leading", true},
		{"trailing/", "trailing", true},
	}
	for _, c := range cases {
		if got := ParseIgnore(c.pattern).Excluded(c.path); got != c.want {
			t.Errorf("pattern %q against %q = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

func TestEmptyIgnoreKeepsEverything(t *testing.T) {
	ig := ParseIgnore("")
	for _, p := range []string{"Dockerfile", "a/b/c", ".env"} {
		if ig.Excluded(p) {
			t.Errorf("%q should be kept by an empty .dockerignore", p)
		}
	}
}
