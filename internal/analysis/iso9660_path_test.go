package analysis

import "testing"

func TestISO9660RelativePath(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple file",
			input:    "sample.bin",
			expected: "sample.bin",
		},
		{
			name:     "nested directories",
			input:    "folder/subdir/sample.bin",
			expected: "folder/subdir/sample.bin",
		},
		{
			name:     "long filename truncated",
			input:    "c22e674d9153f33c60253f4dc77894de7969ddc3098519904b8deb63955c571d.elf",
			expected: "c22e674d9153f33c60253f4d.elf",
		},
		{
			name:     "long directory truncated",
			input:    "superlongdirectorynamewithlotsofcharacters/sample.bin",
			expected: "superlongdirectorynamewithlotso/sample.bin",
		},
		{
			name:     "invalid characters replaced",
			input:    "dir with spaces/sample file!.bin",
			expected: "dir_with_spaces/sample_file!.bin",
		},
		{
			name:     "multiple extensions condensed",
			input:    "archive.tar.gz",
			expected: "archive_tar.gz",
		},
		{
			name:     "extension truncated",
			input:    "sample.longextension",
			expected: "sample.longexte",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := iso9660RelativePath(tc.input)
			if got != tc.expected {
				t.Fatalf("iso9660RelativePath(%q)=%q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}
