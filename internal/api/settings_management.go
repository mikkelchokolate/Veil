package api

import "errors"

var ErrSettingsInvalid = errors.New("settings invalid")

type SettingsManagement struct {
	mutation ManagementStateMutation
}

func NewSettingsManagement(settings *Settings, save func() error) SettingsManagement {
	return SettingsManagement{mutation: NewManagementStateMutation(ManagementStateMutationTarget{Settings: settings}, save)}
}

func (m SettingsManagement) Get() Settings {
	return m.mutation.Settings()
}

func (m SettingsManagement) Update(update Settings) (Settings, error) {
	return m.mutation.UpdateSettings(update)
}
