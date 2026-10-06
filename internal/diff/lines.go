package diff

// Line is one line of a text diff. Op is ' ' (context), '+' (added),
// '-' (removed) or '~' (elided unchanged lines between hunks).
type Line struct {
	Op   byte   `json:"op"`
	Text string `json:"text"`
}

// maxLCSCells bounds the O(n*m) table used by Lines. Beyond it we fall back
// to a whole-block replacement, which is still correct, just less precise.
const maxLCSCells = 4_000_000

// Lines returns a diff of a and b with the given number of context lines
// around each change. Unchanged runs longer than that are elided.
func Lines(a, b []string, context int) []Line {
	ops := lcsOps(a, b)
	return withContext(ops, context)
}

func lcsOps(a, b []string) []Line {
	// Trim common prefix and suffix first; diffs are usually small edits of
	// large documents, so this keeps the LCS table tiny.
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}

	var out []Line
	for _, s := range a[:pre] {
		out = append(out, Line{' ', s})
	}
	out = append(out, lcsMiddle(a[pre:len(a)-suf], b[pre:len(b)-suf])...)
	for _, s := range a[len(a)-suf:] {
		out = append(out, Line{' ', s})
	}
	return out
}

func lcsMiddle(a, b []string) []Line {
	n, m := len(a), len(b)
	var out []Line
	if n*m > maxLCSCells || n == 0 || m == 0 {
		for _, s := range a {
			out = append(out, Line{'-', s})
		}
		for _, s := range b {
			out = append(out, Line{'+', s})
		}
		return out
	}
	// dp[i][j] = LCS length of a[i:] and b[j:].
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else {
				dp[i][j] = max(dp[i+1][j], dp[i][j+1])
			}
		}
	}
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			out = append(out, Line{' ', a[i]})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			out = append(out, Line{'-', a[i]})
			i++
		default:
			out = append(out, Line{'+', b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, Line{'-', a[i]})
	}
	for ; j < m; j++ {
		out = append(out, Line{'+', b[j]})
	}
	return out
}

func withContext(ops []Line, context int) []Line {
	keep := make([]bool, len(ops))
	for i, l := range ops {
		if l.Op == ' ' {
			continue
		}
		for k := max(0, i-context); k <= min(len(ops)-1, i+context); k++ {
			keep[k] = true
		}
	}
	var out []Line
	elided := false
	for i, l := range ops {
		if keep[i] {
			out = append(out, l)
			elided = false
			continue
		}
		if !elided {
			out = append(out, Line{Op: '~'})
			elided = true
		}
	}
	return out
}
