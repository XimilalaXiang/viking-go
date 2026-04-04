package context

import "fmt"

// Role represents the role of a request context.
type Role string

const (
	RoleRoot  Role = "root"
	RoleAdmin Role = "admin"
	RoleUser  Role = "user"
)

// UserIdentifier represents a user with account, user, and agent IDs.
type UserIdentifier struct {
	AccountID string `json:"account_id"`
	UserID    string `json:"user_id"`
	AgentID   string `json:"agent_id"`
}

// UserSpaceName returns the space name for user-scoped data.
func (u *UserIdentifier) UserSpaceName() string {
	return fmt.Sprintf("%s_%s", u.AccountID, u.UserID)
}

// AgentSpaceName returns the space name for agent-scoped data.
func (u *UserIdentifier) AgentSpaceName() string {
	return fmt.Sprintf("%s_%s", u.AccountID, u.AgentID)
}

// DefaultUser returns a default user identifier for development.
func DefaultUser() *UserIdentifier {
	return &UserIdentifier{
		AccountID: "default",
		UserID:    "default",
		AgentID:   "default",
	}
}

// RequestContext carries identity and role for multi-tenant operations.
type RequestContext struct {
	User      *UserIdentifier
	Role      Role
	AccountID string
}

// NewRequestContext creates a new RequestContext.
func NewRequestContext(user *UserIdentifier, role Role) *RequestContext {
	accountID := "default"
	if user != nil {
		accountID = user.AccountID
	}
	return &RequestContext{
		User:      user,
		Role:      role,
		AccountID: accountID,
	}
}

// RootContext returns a root-privileged context.
func RootContext() *RequestContext {
	return NewRequestContext(DefaultUser(), RoleRoot)
}

// DefaultContext returns a default user context.
func DefaultContext() *RequestContext {
	return NewRequestContext(DefaultUser(), RoleUser)
}
