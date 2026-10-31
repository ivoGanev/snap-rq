package runnables

import "snap-rq/backend/commands"

// Catalog is every command the app knows. Read top to bottom, this is the
// list of everything the app can do — the same names the UI, and one day
// an MCP server or CLI, invoke.
var Catalog = []commands.Command{
	// requests
	&GetRequest{},
	&ListRequestSummaries{},
	&ListRequestSummariesForCollection{},
	&ListRequestSummariesForProject{},
	&ListRecentRequests{},
	&ListRequestSummariesByIDs{},
	&CreateRequest{},
	&UpdateRequest{},
	&DeleteRequests{},
	&BulkSetRequestField{},
	&DuplicateRequest{},
	&UpdateRequestOrder{},

	// send flow: external effects and append-only history, never undone
	&SendRequest{},
	&ListResponsesForRequest{},
	&SaveResponse{},
	&SaveResponses{},
	&UpdateResponse{},
	&SaveResponseToFile{},
	&RecordRequestResponse{},
	&RecordRequestUsage{},

	// trash bin
	&ListBinnedRequests{},
	&RestoreBinnedRequests{},
	&DeleteBinnedRequests{},

	// favourites
	&CreateFavouriteCollection{},
	&DeleteFavouriteCollection{},

	// environments and their variables
	&CreateEnvironment{},
	&DeleteEnvironment{},
	&CreateVariable{},
	&DeleteVariable{},

	// curl helpers
	&ParseCurl{},
	&RenderCurl{},
}
