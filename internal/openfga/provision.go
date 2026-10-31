package openfga

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/tjrasche/HerbsfestOS/internal/scheduling"
	"gorm.io/gorm"
)

type binding struct{ StoreID, ModelID, ModelHash string }

func policyHash(model []byte) (string, error) {
	var value any
	if err := json.Unmarshal(model, &value); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(normalizePolicy(value))
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(canonical)
	return hex.EncodeToString(hash[:]), nil
}

// Provision pins the embedded immutable policy in PostgreSQL. It is called only
// by the GitOps preparation container while the web Deployment is stopped.
// A failed transaction may leave an unused store/model, but can never publish
// an incomplete binding. Retries reuse only a successfully committed binding.
func Provision(ctx context.Context, db *gorm.DB, cfg Config) (Config, error) {
	model, err := scheduling.Files.ReadFile("authorization-model.json")
	if err != nil {
		return cfg, err
	}
	hash, err := policyHash(model)
	if err != nil {
		return cfg, err
	}
	client, err := New(cfg)
	if err != nil {
		return cfg, err
	}
	defer client.Close()
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(71420932)").Error; err != nil {
			return err
		}
		var b binding
		if err := tx.Raw("SELECT * FROM authorization_bindings WHERE model_hash=?", hash).Scan(&b).Error; err != nil {
			return err
		}
		if b.StoreID != "" {
			if cfg.StoreID != "" && (cfg.StoreID != b.StoreID || cfg.AuthorizationModelID != b.ModelID) {
				return errors.New("configured authorization IDs conflict with GitOps binding")
			}
			cfg.StoreID, cfg.AuthorizationModelID = b.StoreID, b.ModelID
			return verifyBinding(ctx, cfg, model)
		}
		if cfg.StoreID != "" {
			return errors.New("GitOps provisioning requires no external store/model IDs; migrate an existing binding explicitly")
		}
		// Dedicated store: persisted live grants from another application or a prior
		// experiment can never grant access through this contextual-only model.
		var store struct {
			ID string `json:"id"`
		}
		if err := client.request(ctx, http.MethodPost, "/stores", map[string]string{"name": "herbstfest-volunteer-scheduling"}, &store); err != nil {
			return err
		}
		if !identifier.MatchString(store.ID) {
			return ErrInvalidResponse
		}
		var installed struct {
			ID string `json:"authorization_model_id"`
		}
		if err := client.request(ctx, http.MethodPost, "/stores/"+store.ID+"/authorization-models", json.RawMessage(model), &installed); err != nil {
			return err
		}
		if !identifier.MatchString(installed.ID) {
			return ErrInvalidResponse
		}
		cfg.StoreID, cfg.AuthorizationModelID = store.ID, installed.ID
		if err := verifyBinding(ctx, cfg, model); err != nil {
			return err
		}
		return tx.Exec("INSERT INTO authorization_bindings(model_hash,store_id,model_id) VALUES(?,?,?)", hash, store.ID, installed.ID).Error
	})
	return cfg, err
}

// LoadBinding never creates a store, selects a latest model, or changes a pin.
// The compiled policy hash must have a committed binding from preparation.
func LoadBinding(ctx context.Context, db *gorm.DB, cfg Config) (Config, error) {
	model, err := scheduling.Files.ReadFile("authorization-model.json")
	if err != nil {
		return cfg, err
	}
	hash, err := policyHash(model)
	if err != nil {
		return cfg, err
	}
	var b binding
	if err := db.WithContext(ctx).Raw("SELECT * FROM authorization_bindings WHERE model_hash=?", hash).Scan(&b).Error; err != nil {
		return cfg, errors.New("GitOps authorization preparation is required")
	}
	if !identifier.MatchString(b.StoreID) || !identifier.MatchString(b.ModelID) {
		return cfg, ErrModelNotConfigured
	}
	if cfg.StoreID != "" && (cfg.StoreID != b.StoreID || cfg.AuthorizationModelID != b.ModelID) {
		return cfg, errors.New("configured authorization IDs conflict with GitOps binding")
	}
	cfg.StoreID, cfg.AuthorizationModelID = b.StoreID, b.ModelID
	return cfg, nil
}

func verifyBinding(ctx context.Context, cfg Config, expected []byte) error {
	c, err := New(cfg)
	if err != nil {
		return err
	}
	defer c.Close()
	var response struct {
		Model struct {
			ID     string          `json:"id"`
			Schema string          `json:"schema_version"`
			Types  json.RawMessage `json:"type_definitions"`
		} `json:"authorization_model"`
	}
	if err = c.request(ctx, http.MethodGet, "/stores/"+cfg.StoreID+"/authorization-models/"+cfg.AuthorizationModelID, nil, &response); err != nil {
		return err
	}
	if response.Model.ID != cfg.AuthorizationModelID {
		return ErrInvalidResponse
	}
	actual, _ := json.Marshal(map[string]any{"schema_version": response.Model.Schema, "type_definitions": response.Model.Types})
	a, err := policyHash(actual)
	if err != nil {
		return err
	}
	b, err := policyHash(expected)
	if err != nil {
		return err
	}
	if a != b {
		return errors.New("pinned authorization model differs from compiled policy")
	}
	// A dedicated contextual store must remain empty of persistent grants.
	var tuples struct {
		Tuples []json.RawMessage `json:"tuples"`
	}
	if err = c.request(ctx, http.MethodPost, "/stores/"+cfg.StoreID+"/read", map[string]int{"page_size": 1}, &tuples); err != nil {
		return err
	}
	if len(tuples.Tuples) != 0 {
		return errors.New("contextual authorization store contains persistent tuples")
	}
	return nil
}

// OpenFGA's protobuf JSON includes default strings/null metadata which the
// authored model omits. Retain empty rewrite objects ("this", "wildcard"):
// they carry semantics and must not disappear during normalization.
func normalizePolicy(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, child := range x {
			if child == nil {
				continue
			}
			if text, ok := child.(string); ok && text == "" && (k == "object" || k == "condition" || k == "module") {
				continue
			}
			if m, ok := child.(map[string]any); ok && len(m) == 0 && k == "relations" {
				continue
			}
			out[k] = normalizePolicy(child)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, child := range x {
			out[i] = normalizePolicy(child)
		}
		return out
	default:
		return v
	}
}
