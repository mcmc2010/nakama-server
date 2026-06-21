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
	"errors"
	"strings"

	"github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func LinkApple(ctx context.Context, logger *zap.Logger, db *sql.DB, config Config, userID uuid.UUID, token string) error {
	return status.Error(codes.Unimplemented, "Apple linking is not supported.")
}

func LinkCustom(ctx context.Context, logger *zap.Logger, db *sql.DB, userID uuid.UUID, customID string) error {
	if customID == "" {
		return status.Error(codes.InvalidArgument, "Custom ID is required.")
	} else if invalidCharsRegex.MatchString(customID) {
		return status.Error(codes.InvalidArgument, "Invalid custom ID, no spaces or control characters allowed.")
	} else if len(customID) < 6 || len(customID) > 128 {
		return status.Error(codes.InvalidArgument, "Invalid custom ID, must be 6-128 bytes.")
	}

	res, err := db.ExecContext(ctx, `
UPDATE users
SET custom_id = $2, update_time = now()
WHERE (id = $1)
AND (NOT EXISTS
    (SELECT id
     FROM users
     WHERE custom_id = $2 AND NOT id = $1))`,
		userID,
		customID)

	if err != nil {
		logger.Error("Could not link custom ID.", zap.Error(err), zap.Any("input", customID))
		return status.Error(codes.Internal, "Error while trying to link Custom ID.")
	} else if count, _ := res.RowsAffected(); count == 0 {
		return status.Error(codes.AlreadyExists, "Custom ID is already in use.")
	}
	return nil
}

func LinkDevice(ctx context.Context, logger *zap.Logger, db *sql.DB, userID uuid.UUID, deviceID string) error {
	if deviceID == "" {
		return status.Error(codes.InvalidArgument, "Device ID is required.")
	} else if invalidCharsRegex.MatchString(deviceID) {
		return status.Error(codes.InvalidArgument, "Device ID invalid, no spaces or control characters allowed.")
	} else if len(deviceID) < 10 || len(deviceID) > 128 {
		return status.Error(codes.InvalidArgument, "Device ID invalid, must be 10-128 bytes.")
	}

	err := ExecuteInTx(ctx, db, func(tx *sql.Tx) error {
		var dbDeviceIDLinkedUser int64
		err := tx.QueryRowContext(ctx, "SELECT COUNT(id) FROM user_device WHERE id = $1 AND user_id = $2 LIMIT 1", deviceID, userID).Scan(&dbDeviceIDLinkedUser)
		if err != nil {
			logger.Debug("Cannot link device ID.", zap.Error(err), zap.Any("input", deviceID))
			return err
		}

		if dbDeviceIDLinkedUser == 0 {
			_, err = tx.ExecContext(ctx, "INSERT INTO user_device (id, user_id) VALUES ($1, $2)", deviceID, userID)
			if err != nil {
				var pgErr *pgconn.PgError
				if errors.As(err, &pgErr) && pgErr.Code == dbErrorUniqueViolation {
					return StatusError(codes.AlreadyExists, "Device ID already in use.", err)
				}
				logger.Debug("Cannot link device ID.", zap.Error(err), zap.Any("input", deviceID))
				return err
			}
		}

		_, err = tx.ExecContext(ctx, "UPDATE users SET update_time = now() WHERE id = $1", userID)
		if err != nil {
			logger.Debug("Cannot update users table while linking.", zap.Error(err), zap.Any("input", deviceID))
			return err
		}
		return nil
	})

	if err != nil {
		if e, ok := err.(*statusError); ok {
			return e.Status()
		}
		logger.Error("Error in database transaction.", zap.Error(err))
		return status.Error(codes.Internal, "Error linking Device ID.")
	}
	return nil
}

func LinkEmail(ctx context.Context, logger *zap.Logger, db *sql.DB, userID uuid.UUID, email, password string) error {
	if email == "" || password == "" {
		return status.Error(codes.InvalidArgument, "Email address and password is required.")
	} else if invalidCharsRegex.MatchString(email) {
		return status.Error(codes.InvalidArgument, "Invalid email address, no spaces or control characters allowed.")
	} else if len(password) < 8 {
		return status.Error(codes.InvalidArgument, "Password must be at least 8 characters long.")
	} else if !emailRegex.MatchString(email) {
		return status.Error(codes.InvalidArgument, "Invalid email address format.")
	} else if len(email) < 10 || len(email) > 255 {
		return status.Error(codes.InvalidArgument, "Invalid email address, must be 10-255 bytes.")
	}

	cleanEmail := strings.ToLower(email)
	hashedPassword, _ := bcrypt.GenerateFromPassword([]byte(password), bcryptHashCost)

	res, err := db.ExecContext(ctx, `
UPDATE users
SET email = $2, password = $3, update_time = now()
WHERE (id = $1)
AND (NOT EXISTS
    (SELECT id
     FROM users
     WHERE email = $2 AND NOT id = $1))`,
		userID,
		cleanEmail,
		hashedPassword)

	if err != nil {
		logger.Error("Could not link email.", zap.Error(err), zap.Any("input", email))
		return status.Error(codes.Internal, "Error while trying to link email.")
	} else if count, _ := res.RowsAffected(); count == 0 {
		return status.Error(codes.AlreadyExists, "Email is already in use.")
	}
	return nil
}

func LinkFacebook(ctx context.Context, logger *zap.Logger, db *sql.DB, userID uuid.UUID, username, token string, sync bool) error {
	return status.Error(codes.Unimplemented, "Facebook linking is not supported.")
}

func LinkFacebookInstantGame(ctx context.Context, logger *zap.Logger, db *sql.DB, config Config, userID uuid.UUID, signedPlayerInfo string) error {
	return status.Error(codes.Unimplemented, "Facebook Instant Game linking is not supported.")
}

func LinkGameCenter(ctx context.Context, logger *zap.Logger, db *sql.DB, userID uuid.UUID, playerID string, bundleID string, timestamp int64, salt string, signature string, publicKeyURL string) error {
	return status.Error(codes.Unimplemented, "Game Center linking is not supported.")
}

func LinkGoogle(ctx context.Context, logger *zap.Logger, db *sql.DB, userID uuid.UUID, idToken string) error {
	return status.Error(codes.Unimplemented, "Google linking is not supported.")
}

func LinkSteam(ctx context.Context, logger *zap.Logger, db *sql.DB, config Config, userID uuid.UUID, username, token string, sync bool) error {
	return status.Error(codes.Unimplemented, "Steam linking is not supported.")
}
