package auth

import "fmt"

// Role represents a user's access level in the system.
type Role string

const (
	RoleAdmin    Role = "admin"
	RoleOperator Role = "operator"
	RoleViewer   Role = "viewer"
)

// roleLevel maps each role to a numeric level for hierarchy comparison.
var roleLevel = map[Role]int{
	RoleViewer:   1,
	RoleOperator: 2,
	RoleAdmin:    3,
}

// Valid returns true if the role is a known role.
func (r Role) Valid() bool {
	_, ok := roleLevel[r]
	return ok
}

// Level returns the numeric level of this role (higher = more access).
func (r Role) Level() int {
	return roleLevel[r]
}

// AtLeast returns true if this role has at least the given minimum access level.
func (r Role) AtLeast(min Role) bool {
	return r.Level() >= min.Level()
}

// ParseRole converts a string to a Role, returning an error if invalid.
func ParseRole(s string) (Role, error) {
	r := Role(s)
	if !r.Valid() {
		return "", fmt.Errorf("invalid role %q: must be admin, operator, or viewer", s)
	}
	return r, nil
}
