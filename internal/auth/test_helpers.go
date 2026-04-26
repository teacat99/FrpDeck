package auth

import "github.com/teacat99/FrpDeck/internal/model"

// IssueTestToken mints a signed JWT for the given user using the same
// signing helper that LoginHandler uses in production. This exists for
// tests in other packages (api/ws_test.go) that need a valid token but
// do not want to walk the full login HTTP path.
//
// The helper is namespaced "test" so its non-production purpose is
// obvious to readers; we keep it in the regular file (not _test.go) so
// it is reachable from tests in other packages that import this one.
func IssueTestToken(a *Authenticator, u *model.User) (string, error) {
	return a.sign(u)
}
