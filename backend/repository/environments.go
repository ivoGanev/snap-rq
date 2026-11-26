// Package repository reads models from the database. Callers say what to
// load with them; nothing here knows how a model is used.
package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"snap-rq/backend/models"
	"snap-rq/backend/secrets"
)

// Querier is a database or a transaction.
type Querier interface {
	secrets.Querier
	Query(query string, args ...any) (*sql.Rows, error)
}

// Secrets says what happens to secret values when variables are loaded.
type Secrets int

const (
	// KeepEncrypted leaves secret values as stored.
	KeepEncrypted Secrets = iota
	// Decrypt opens them; a secret the vault can't open comes back Locked.
	Decrypt
)

// Load says what to load with environments.
type Load struct {
	Variables bool
	Secrets   Secrets
}

type Environments struct{ q Querier }

func NewEnvironments(q Querier) Environments { return Environments{q: q} }

// Get loads one environment. Environment 0 (none) is an empty environment,
// so callers needn't treat "no environment selected" apart.
func (r Environments) Get(id int64, opts Load) (models.Environment, error) {
	if id == 0 {
		return models.Environment{}, nil
	}
	envs, err := r.load(`id = ?`, []any{id}, opts)
	if err != nil {
		return models.Environment{}, err
	}
	if len(envs) == 0 {
		return models.Environment{}, fmt.Errorf("environment %d not found", id)
	}
	return envs[0], nil
}

// GetMany loads the environments with the given ids, in id order.
func (r Environments) GetMany(ids []int64, opts Load) ([]models.Environment, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return r.load(`id IN (?`+strings.Repeat(`, ?`, len(ids)-1)+`)`, args, opts)
}

// ListForProject loads a project's environments, in id order.
func (r Environments) ListForProject(projectID int64, opts Load) ([]models.Environment, error) {
	return r.load(`project_id = ?`, []any{projectID}, opts)
}

// load runs two queries however many environments match: the environments,
// then all their variables, matched by the same condition.
func (r Environments) load(where string, args []any, opts Load) ([]models.Environment, error) {
	rows, err := r.q.Query(`SELECT id, project_id, name, created_at FROM environments
		WHERE `+where+` ORDER BY id`, args...)
	if err != nil {
		return nil, fmt.Errorf("loading environments: %w", err)
	}
	var envs []models.Environment
	index := map[int64]int{}
	for rows.Next() {
		var e models.Environment
		if err := rows.Scan(&e.ID, &e.ProjectID, &e.Name, &e.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		index[e.ID] = len(envs)
		envs = append(envs, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !opts.Variables || len(envs) == 0 {
		return envs, nil
	}

	rows, err = r.q.Query(`SELECT id, environment_id, key, value, secret FROM environment_variables
		WHERE environment_id IN (SELECT id FROM environments WHERE `+where+`)
		ORDER BY environment_id, id`, args...)
	if err != nil {
		return nil, fmt.Errorf("loading variables: %w", err)
	}
	defer rows.Close()
	vaultLocked := false
	for rows.Next() {
		var v models.EnvironmentVariable
		if err := rows.Scan(&v.ID, &v.EnvironmentID, &v.Key, &v.Value, &v.Secret); err != nil {
			return nil, err
		}
		v.HasValue = v.Value != ""
		if v.Secret && v.HasValue && opts.Secrets == Decrypt {
			vaultLocked = r.decrypt(&v, vaultLocked)
		}
		e := &envs[index[v.EnvironmentID]]
		e.Variables = append(e.Variables, v)
	}
	return envs, rows.Err()
}

// decrypt opens one secret in place. Once the vault is found locked, the
// rest of the load marks secrets Locked without asking it again.
func (r Environments) decrypt(v *models.EnvironmentVariable, vaultLocked bool) bool {
	if vaultLocked {
		v.Value, v.Locked = "", true
		return true
	}
	plain, err := secrets.Default().Decrypt(r.q, v.Value)
	if err != nil {
		v.Value, v.Locked = "", true
		return errors.Is(err, secrets.ErrLocked)
	}
	v.Value = plain
	return false
}
