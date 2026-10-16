// Package auth manages users, roles, sessions and login protection.
package auth

import "slices"

// Role is a fixed set of permissions.
type Role string

// Roles, from least to most privileged.
const (
	RoleViewer    Role = "viewer"
	RoleOperator  Role = "operator"
	RoleInstaller Role = "installer"
	RoleOwner     Role = "owner"
)

// Permission is one action a role may take.
type Permission string

// Permissions.
const (
	PermDashboardView Permission = "dashboard.view"
	PermEnergyView    Permission = "energy.view"
	PermDevicesView   Permission = "devices.view"
	PermAlarmsView    Permission = "alarms.view"

	PermAlarmsAck       Permission = "alarms.ack"
	PermControlSetpoint Permission = "control.setpoint" // export limit within the installer's bounds

	PermControlConfigure Permission = "control.configure" // mode, limits, margins, fail-safe
	PermSiteConfigure    Permission = "site.configure"    // buses, devices, site details
	PermCommissioning    Permission = "commissioning"     // raw Modbus reads, device probes
	PermAuditView        Permission = "audit.view"
	PermSystemReboot     Permission = "system.reboot"

	PermUsersManage  Permission = "users.manage"
	PermSystemUpdate Permission = "system.update"
)

var rolePermissions = map[Role][]Permission{
	RoleViewer: {PermDashboardView, PermEnergyView, PermDevicesView, PermAlarmsView},
	RoleOperator: {PermDashboardView, PermEnergyView, PermDevicesView, PermAlarmsView,
		PermAlarmsAck, PermControlSetpoint},
	RoleInstaller: {PermDashboardView, PermEnergyView, PermDevicesView, PermAlarmsView,
		PermAlarmsAck, PermControlSetpoint,
		PermControlConfigure, PermSiteConfigure, PermCommissioning, PermAuditView, PermSystemReboot},
	RoleOwner: {PermDashboardView, PermEnergyView, PermDevicesView, PermAlarmsView,
		PermAlarmsAck, PermControlSetpoint,
		PermControlConfigure, PermSiteConfigure, PermCommissioning, PermAuditView, PermSystemReboot,
		PermUsersManage, PermSystemUpdate},
}

// Valid reports whether r is a known role.
func (r Role) Valid() bool {
	_, ok := rolePermissions[r]
	return ok
}

// Can reports whether the role grants p.
func (r Role) Can(p Permission) bool {
	return slices.Contains(rolePermissions[r], p)
}

// Permissions lists what the role grants.
func (r Role) Permissions() []Permission {
	return slices.Clone(rolePermissions[r])
}
