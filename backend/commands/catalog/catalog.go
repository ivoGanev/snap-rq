// Package catalog lists every command the app knows. Read top to bottom,
// this is everything the app can do.
package catalog

import (
	"snap-rq/backend/commands"
	"snap-rq/backend/commands/bin"
	"snap-rq/backend/commands/collections"
	"snap-rq/backend/commands/curl"
	"snap-rq/backend/commands/environments"
	"snap-rq/backend/commands/favourites"
	"snap-rq/backend/commands/icons"
	"snap-rq/backend/commands/profiles"
	"snap-rq/backend/commands/projects"
	"snap-rq/backend/commands/requests"
	"snap-rq/backend/commands/responses"
	"snap-rq/backend/commands/settings"
	"snap-rq/backend/commands/tags"
	"snap-rq/backend/commands/variables"
)

// All is every command factory, registered once at startup.
var All = []commands.Factory{
	// requests
	requests.NewGet,
	requests.NewListSummaries,
	requests.NewListForCollection,
	requests.NewListForProject,
	requests.NewListRecent,
	requests.NewListByIDs,
	requests.NewCreate,
	requests.NewUpdate,
	requests.NewDelete,
	requests.NewDuplicate,
	requests.NewReorder,
	requests.NewBulkSet,

	// response history and export (the send itself is an observable command)
	responses.NewList,
	responses.NewSaveToFile,

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
	tags.NewListRequests,
	tags.NewList,
	tags.NewListForRequests,
	tags.NewAdd,
	tags.NewRemove,
	tags.NewDelete,
	tags.NewRename,
	tags.NewSetAppearance,
	tags.NewReorder,
	tags.NewSetColor,

	// environments and their variables
	environments.NewList,
	environments.NewCreate,
	environments.NewUpdate,
	environments.NewDelete,
	variables.NewList,
	variables.NewCreate,
	variables.NewUpdate,
	variables.NewDelete,

	// settings and upkeep
	settings.NewGet,
	settings.NewUpdate,
	icons.NewCleanup,

	// curl helpers
	curl.NewParse,
	curl.NewRender,
}
