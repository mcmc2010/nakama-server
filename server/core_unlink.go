// Copyright 2020 The Nakama Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package server

import (
	"context"
	"database/sql"
	"strings"

	"github.com/gofrs/uuid/v5"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func UnlinkApple(ctx context.Context, logger *zap.Logger, db *sql.DB, config Config, id uuid.UUID, token string) error {
	return status.Error(codes.Unimplemented, "Apple unlinking is not supported.")
}

func UnlinkCustom(ctx context.Context, logger *zap.Logger, db *sql.DB, id uuid.UUID, customID string) error {
	params := []any{id}
	query := `UPDATE users SET custom_id = NULL, update_time = now() WHERE id = $1`

	if customID != "" {
		params = append(params, customID)
		query = query + ` AND custom_id = $2`
	}

	query = query +
		` AND ((email IS NOT NULL)
     OR
     EXISTS (SELECT id FROM user_device WHERE user_id = $1 LIMIT 1))`

	res, err := db.ExecContext(ctx, query, params...)

	if err != nil {
		logger.Error("Could not unlink custom ID.", zap.Error(err), zap.Any("input", customID))
		return status.Error(codes.Internal, "Error while trying to unlink custom ID.")
	} else if count, _ := res.RowsAffected(); count == 0 {
		return status.Error(codes.PermissionDenied, "Cannot unlink last account identifier. Check profile exists and is not last link.")
	}
	return nil
}

func UnlinkDevice(ctx context.Context, logger *zap.Logger, db *sql.DB, id uuid.UUID, deviceID string) error {
	if deviceID == "" {
		return status.Error(codes.InvalidArgument, "A device ID must be supplied.")
	}

	err := ExecuteInTx(ctx, db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM user_device WHERE id = $2 AND user_id = $1
AND (EXISTS (SELECT id FROM users WHERE id = $1 AND
    (email IS NOT NULL
     OR custom_id IS NOT NULL))
   OR EXISTS (SELECT id FROM user_device WHERE user_id = $1 AND id <> $2 LIMIT 1))`, id, deviceID)
		if err != nil {
			logger.Debug("Could not unlink device ID.", zap.Error(err), zap.Any("input", deviceID))
			return err
		}
		if count, _ := res.RowsAffected(); count == 0 {
			return StatusError(codes.PermissionDenied, "Cannot unlink last account identifier. Check profile exists and is not last link.", ErrRowsAffectedCount)
		}

		res, err = tx.ExecContext(ctx, "UPDATE users SET update_time = now() WHERE id = $1", id)
		if err != nil {
			logger.Debug("Could not unlink device ID.", zap.Error(err), zap.Any("input", deviceID))
			return err
		}
		if count, _ := res.RowsAffected(); count == 0 {
			return StatusError(codes.PermissionDenied, "Cannot unlink last account identifier. Check profile exists and is not last link.", ErrRowsAffectedCount)
		}

		return nil
	})

	if err != nil {
		if e, ok := err.(*statusError); ok {
			return e.Status()
		}
		logger.Error("Error in database transaction.", zap.Error(err))
		return status.Error(codes.Internal, "Could not unlink device ID.")
	}
	return nil
}

func UnlinkEmail(ctx context.Context, logger *zap.Logger, db *sql.DB, id uuid.UUID, email string) error {
	params := []any{id}
	query := `UPDATE users SET email = NULL, password = NULL, update_time = now() WHERE id = $1`

	if email != "" {
		cleanEmail := strings.ToLower(email)
		params = append(params, cleanEmail)
		query = query + ` AND email = $2`
	}

	query = query +
		` AND ((custom_id IS NOT NULL)
     OR
     EXISTS (SELECT id FROM user_device WHERE user_id = $1 LIMIT 1))`

	res, err := db.ExecContext(ctx, query, params...)

	if err != nil {
		logger.Error("Could not unlink email.", zap.Error(err), zap.Any("input", email))
		return status.Error(codes.Internal, "Error while trying to unlink email.")
	} else if count, _ := res.RowsAffected(); count == 0 {
		return status.Error(codes.PermissionDenied, "Cannot unlink last account identifier. Check profile exists and is not last link.")
	}
	return nil
}

func UnlinkFacebook(ctx context.Context, logger *zap.Logger, db *sql.DB, id uuid.UUID, token string) error {
	return status.Error(codes.Unimplemented, "Facebook unlinking is not supported.")
}

func UnlinkFacebookInstantGame(ctx context.Context, logger *zap.Logger, db *sql.DB, config Config, id uuid.UUID, signedPlayerInfo string) error {
	return status.Error(codes.Unimplemented, "Facebook Instant Game unlinking is not supported.")
}

func UnlinkGameCenter(ctx context.Context, logger *zap.Logger, db *sql.DB, id uuid.UUID, playerID string, bundleID string, timestamp int64, salt string, signature string, publicKeyURL string) error {
	return status.Error(codes.Unimplemented, "Game Center unlinking is not supported.")
}

func UnlinkGoogle(ctx context.Context, logger *zap.Logger, db *sql.DB, id uuid.UUID, token string) error {
	return status.Error(codes.Unimplemented, "Google unlinking is not supported.")
}

func UnlinkSteam(ctx context.Context, logger *zap.Logger, db *sql.DB, config Config, id uuid.UUID, token string) error {
	return status.Error(codes.Unimplemented, "Steam unlinking is not supported.")
}
