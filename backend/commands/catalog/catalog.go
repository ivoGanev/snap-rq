// Package catalog lists every command the app knows. Read top to bottom,
// this is everything the app can do.
package catalog

import (
	"snap-rq/backend/commands"
	"snap-rq/backend/commands/backups"
	"snap-rq/backend/commands/bin"
	"snap-rq/backend/commands/collections"
	"snap-rq/backend/commands/cookiejar"
	"snap-rq/backend/commands/curl"
	"snap-rq/backend/commands/environments"
	"snap-rq/backend/commands/favourites"
	"snap-rq/backend/commands/icons"
	"snap-rq/backend/commands/openapi"
	"snap-rq/backend/commands/profiles"
	"snap-rq/backend/commands/projects"
	"snap-rq/backend/commands/requests"
	"snap-rq/backend/commands/responses"
	"snap-rq/backend/commands/scripts"
	"snap-rq/backend/commands/settings"
	"snap-rq/backend/commands/variables"
	"snap-rq/backend/commands/vault"
)

// All is every command factory, registered once at startup.
var All = []commands.Factory{
	// requests
	requests.NewGet,
	requests.NewTryScript,
	requests.NewListSummaries,
	requests.NewListForCollection,
	requests.NewListForProject,
	requests.NewListRecent,
	requests.NewListUsage,
	requests.NewListByIDs,
	requests.NewCreate,
	requests.NewUpdate,
	requests.NewDelete,
	requests.NewDuplicate,
	requests.NewReorder,
	requests.NewBulkSet,
	requests.NewEditMany,
	requests.NewCreateMany,

	// response history and export (the send itself is an observable command)
	responses.NewList,
	responses.NewSaveToFile,
	responses.NewClear,

	// trash bin
	bin.NewList,
	bin.NewRestore,
	bin.NewDeletePermanently,

	// collections
	collections.NewList,
	collections.NewCreate,
	collections.NewUpdate,
	collections.NewDelete,
	collections.NewSetAppearance,

	// projects and profiles
	projects.NewList,
	projects.NewCreate,
	projects.NewUpdate,
	projects.NewDelete,
	profiles.NewList,
	profiles.NewCreate,
	profiles.NewUpdate,
	profiles.NewDelete,

	// favourites
	favourites.NewSetColor,
	favourites.NewListMemberships,

	favourites.NewList,
	favourites.NewListRequests,
	favourites.NewCreate,
	favourites.NewUpdate,
	favourites.NewDelete,
	favourites.NewSetAppearance,
	favourites.NewAddRequest,
	favourites.NewRemoveRequests,
	favourites.NewReorder,

	// tags
	scripts.NewListRequests,
	scripts.NewList,
	scripts.NewListForRequests,
	scripts.NewGet,
	scripts.NewAttach,
	scripts.NewDetach,
	scripts.NewDelete,
	scripts.NewRename,
	scripts.NewSetContent,
	scripts.NewCheck,
	scripts.NewSetAppearance,
	scripts.NewReorder,
	scripts.NewSetColor,

	// environments and their variables
	environments.NewList,
	environments.NewCreate,
	environments.NewUpdate,
	environments.NewDelete,
	variables.NewList,
	variables.NewCreate,
	variables.NewUpdate,
	variables.NewDelete,
	variables.NewSetSecret,
	variables.NewReveal,
	backups.NewSettings,
	backups.NewUpdateSettings,
	backups.NewList,
	backups.NewDelete,
	vault.NewStatus,
	vault.NewRecoveryKey,
	vault.NewSaveRecoveryKey,
	vault.NewRecoveryKeyReminder,
	vault.NewMarkRecoveryKeySeen,
	vault.NewRestore,
	vault.NewReset,

	// cookie jar
	cookiejar.NewList,
	cookiejar.NewSave,
	cookiejar.NewDelete,
	cookiejar.NewClear,

	// settings and upkeep
	settings.NewGet,
	settings.NewUpdate,
	icons.NewCleanup,

	// curl helpers
	curl.NewParse,
	curl.NewRender,

	// openapi import/export
	openapi.NewPreview,
	openapi.NewImport,
	openapi.NewExport,
}
