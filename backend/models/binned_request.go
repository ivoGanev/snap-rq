package models

import "encoding/json"

// HttpBinnedRequestSummary is a lightweight projection of a binned request for
// list views. The ID field here is the binned row id, not the original request id.
// CollectionID and ProjectID mirror the original values so the binned summary is
// structurally compatible with HttpRequestSummary where needed.
type HttpBinnedRequestSummary struct {
	ID                     int64  `json:"id"`
	OriginalRequestID      int64  `json:"original_request_id"`
	ProjectID              int64  `json:"project_id"`
	CollectionID           int64  `json:"collection_id"`
	OriginalCollectionID   int64  `json:"original_collection_id"`
	OriginalCollectionName string `json:"original_collection_name"`
	Name                   string `json:"name"`
	URL                    string `json:"url"`
	Method                 string `json:"method"`
	StatusCode             int    `json:"status_code"`
	ResponseID             int64  `json:"response_id"`
	DeletedAt              string `json:"deleted_at"`
}

// RestoreBinnedRequestInput pairs a binned row id with the collection the user
// wants to restore the request into.
type RestoreBinnedRequestInput struct {
	BinID              int64 `json:"bin_id"`
	TargetCollectionID int64 `json:"target_collection_id"`
}

// BinnedRequestSnapshot captures the related data that must be recreated when a
// binned request is restored.
type BinnedRequestSnapshot struct {
	Responses []BinnedResponseSnapshot `json:"responses"`
	Tags      []BinnedTagSnapshot      `json:"tags"`
	Favourites []BinnedFavouriteSnapshot `json:"favourites"`
}

// BinnedResponseSnapshot is a flattened copy of a response row.
type BinnedResponseSnapshot struct {
	ID         int64  `json:"id"`
	Headers    string `json:"headers"`
	StatusCode int    `json:"status_code"`
	Body       string `json:"body"`
	CreatedAt  string `json:"created_at"`
	DurationMs int64  `json:"duration_ms"`
}

// BinnedTagSnapshot stores both the tag id and name so we can try to match by
// name if the original tag was deleted and later recreated.
type BinnedTagSnapshot struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// BinnedFavouriteSnapshot stores both the favourite collection id and name for
// best-effort restoration.
type BinnedFavouriteSnapshot struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// MarshalJSON serialises the snapshot to a JSON string.
func (s BinnedRequestSnapshot) MarshalJSON() (string, error) {
	bytes, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// UnmarshalBinnedRequestSnapshot parses a JSON snapshot string.
func UnmarshalBinnedRequestSnapshot(raw string) (BinnedRequestSnapshot, error) {
	var snapshot BinnedRequestSnapshot
	err := json.Unmarshal([]byte(raw), &snapshot)
	return snapshot, err
}
