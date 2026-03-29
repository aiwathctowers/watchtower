package inbox

import "testing"

func TestCleanSnippet(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "user mention with display name",
			in:   "hello <@U010T16N5LN|Maksym Yukhno> how are you",
			want: "hello @Maksym Yukhno how are you",
		},
		{
			name: "user mention without display name",
			in:   "hello <@U010T16N5LN> how are you",
			want: "hello how are you",
		},
		{
			name: "channel reference",
			in:   "check <#C01234567|general> for updates",
			want: "check #general for updates",
		},
		{
			name: "link with display text",
			in:   "see <https://example.com|this link>",
			want: "see this link",
		},
		{
			name: "bare url",
			in:   "visit <https://example.com/foo>",
			want: "visit https://example.com/foo",
		},
		{
			name: "special mention here",
			in:   "<!here|here> please review",
			want: "@here please review",
		},
		{
			name: "special mention channel",
			in:   "<!channel> important",
			want: "@channel important",
		},
		{
			name: "subteam mention",
			in:   "cc <!subteam^S01234|@backend-team>",
			want: "cc @backend-team",
		},
		{
			name: "emoji stripped",
			in:   "great job :thumbsup: :tada:",
			want: "great job",
		},
		{
			name: "code block stripped",
			in:   "before ```some code``` after",
			want: "before after",
		},
		{
			name: "html entities",
			in:   "foo &amp; bar &lt;tag&gt;",
			want: "foo & bar <tag>",
		},
		{
			name: "multiple user mentions with names",
			in:   "<@U111|Alice> and <@U222|Bob> discussed",
			want: "@Alice and @Bob discussed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanSnippet(tt.in)
			if got != tt.want {
				t.Errorf("cleanSnippet(%q)\n  got:  %q\n  want: %q", tt.in, got, tt.want)
			}
		})
	}
}
