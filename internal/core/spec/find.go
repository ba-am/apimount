package spec

import (
	"fmt"
	"strings"
)

// FindOperation matches a concrete request (method + concrete path) against the
// spec and returns the matching Operation plus any resolved path-parameter values.
//
// Matching rules:
//   - Method must match case-insensitively.
//   - Path is matched segment-by-segment against each operation's templated path.
//     A "{name}" segment in the template matches any non-slash value in the request.
//   - If multiple operations match, the one with the most literal (non-template)
//     segments wins. On a tie, the first declared operation wins.
//
// Returns an error if no operation matches.
func FindOperation(ps *ParsedSpec, method, concretePath string) (*Operation, map[string]string, error) {
	if ps == nil {
		return nil, nil, fmt.Errorf("nil ParsedSpec")
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	concretePath = "/" + strings.Trim(concretePath, "/")
	reqSegs := strings.Split(concretePath, "/")

	var best *Operation
	var bestParams map[string]string
	bestLiterals := -1

	for i := range ps.Operations {
		op := &ps.Operations[i]
		if !strings.EqualFold(op.Method, method) {
			continue
		}
		tmpl := "/" + strings.Trim(op.Path, "/")
		tmplSegs := strings.Split(tmpl, "/")
		if len(tmplSegs) != len(reqSegs) {
			continue
		}

		params := map[string]string{}
		literals := 0
		ok := true
		for j, seg := range tmplSegs {
			if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
				params[strings.TrimSuffix(strings.TrimPrefix(seg, "{"), "}")] = reqSegs[j]
				continue
			}
			if seg != reqSegs[j] {
				ok = false
				break
			}
			literals++
		}
		if !ok {
			continue
		}
		if literals > bestLiterals {
			best = op
			bestParams = params
			bestLiterals = literals
		}
	}

	if best == nil {
		return nil, nil, fmt.Errorf("no operation in spec matches %s %s", method, concretePath)
	}
	return best, bestParams, nil
}
