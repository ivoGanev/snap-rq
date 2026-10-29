package services

import "database/sql"

// selectIDsTx returns the ids produced by a single-column id query, inside
// the command's transaction. Used to enumerate cascade victims before a
// delete so their before-images land in the memento.
func selectIDsTx(tx *sql.Tx, query string, args ...any) ([]int64, error) {
	rows, err := tx.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
