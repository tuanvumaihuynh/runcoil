package api

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/runcoil/runcoil/internal/api/gen"
	"github.com/runcoil/runcoil/internal/api/middleware"
	"github.com/runcoil/runcoil/internal/domain"
	"github.com/runcoil/runcoil/internal/services"
)

type apiKeyHandler struct {
	apiKeyService services.APIKeyService
}

func newAPIKeyHandler(apiKeyService services.APIKeyService) *apiKeyHandler {
	return &apiKeyHandler{apiKeyService: apiKeyService}
}

func (h *apiKeyHandler) ListApiKeys(ctx context.Context, req gen.ListApiKeysRequestObject) (gen.ListApiKeysResponseObject, error) {
	res, err := h.apiKeyService.ListAPIKeys(ctx, uuid.UUID(req.ProjectId))
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}

	keys := make([]gen.ApiKey, 0, len(res))
	for _, key := range res {
		keys = append(keys, apiKeyToResponse(key))
	}

	return gen.ListApiKeys200JSONResponse{ApiKeys: keys}, nil
}

func (h *apiKeyHandler) CreateApiKey(ctx context.Context, req gen.CreateApiKeyRequestObject) (gen.CreateApiKeyResponseObject, error) {
	identity, ok := middleware.IdentityFromContext(ctx)
	if !ok {
		return nil, errors.New("no identity in the context")
	}

	res, err := h.apiKeyService.CreateAPIKey(ctx, services.CreateAPIKeyParams{
		ProjectID: uuid.UUID(req.ProjectId),
		Name:      req.Body.Name,
		CreatedBy: identity.UserID,
	})
	if err != nil {
		return nil, fmt.Errorf("create api key: %w", err)
	}

	return gen.CreateApiKey201JSONResponse{ApiKey: apiKeyToResponse(res.APIKey), Key: res.Key}, nil
}

func (h *apiKeyHandler) RevokeApiKey(ctx context.Context, req gen.RevokeApiKeyRequestObject) (gen.RevokeApiKeyResponseObject, error) {
	err := h.apiKeyService.RevokeAPIKey(ctx, services.RevokeAPIKeyParams{
		ProjectID: uuid.UUID(req.ProjectId),
		APIKeyID:  uuid.UUID(req.ApiKeyId),
	})
	if err != nil {
		return nil, fmt.Errorf("revoke api key: %w", err)
	}

	return gen.RevokeApiKey204Response{}, nil
}

func apiKeyToResponse(key domain.APIKey) gen.ApiKey {
	return gen.ApiKey{
		Id:         openapi_types.UUID(key.ID),
		Name:       key.Name,
		KeyHint:    key.KeyHint,
		CreatedBy:  key.CreatedBy,
		CreatedAt:  key.CreatedAt,
		LastUsedAt: key.LastUsedAt,
		RevokedAt:  key.RevokedAt,
	}
}
