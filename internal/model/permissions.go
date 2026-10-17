package model

import "fmt"

// Business permissions apply only to locally authenticated self-hosted users.
var BusinessPermissions = []string{
	"samples.write", "data.manage", "tasks.manage", "pipelines.manage",
	"gene_lists.manage", "bed.manage", "baselines.manage", "report_services.manage",
	"interpretation_reports.manage",
}

func NormalizePermissions(values []string) ([]string, error) {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		valid := false
		for _, allowed := range BusinessPermissions {
			if value == allowed {
				valid = true
				break
			}
		}
		if !valid {
			return nil, fmt.Errorf("invalid permission: %s", value)
		}
		seen[value] = true
	}
	for _, value := range BusinessPermissions {
		if seen[value] {
			out = append(out, value)
		}
	}
	return out, nil
}

func (u *User) EffectivePermissions() []string {
	if u.SystemRole == SystemRoleSuperAdmin {
		return append([]string{}, BusinessPermissions...)
	}
	values, err := NormalizePermissions(u.Permissions)
	if err != nil {
		return []string{}
	}
	return values
}

func HasPermission(values []string, permission string) bool {
	for _, value := range values {
		if value == permission {
			return true
		}
	}
	return false
}
