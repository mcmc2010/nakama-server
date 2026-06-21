// Copyright 2018 The Nakama Authors
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
	"math"
	"strings"

	"github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var bcryptHashCost = int(math.Max(float64(bcrypt.DefaultCost), 13)) // Currently recommended minimum cost is 13.

func AuthenticateCustom(ctx context.Context, logger *zap.Logger, db *sql.DB, customID, username string, create bool) (string, string, bool, error) {
	found := true

	// Look for an existing account.
	query := "SELECT id, username, disable_time FROM users WHERE custom_id = $1"
	var dbUserID string
	var dbUsername string
	var dbDisableTime pgtype.Timestamptz
	err := db.QueryRowContext(ctx, query, customID).Scan(&dbUserID, &dbUsername, &dbDisableTime)
	if err != nil {
		if err == sql.ErrNoRows {
			found = false
		} else {
			logger.Error("Error looking up user by custom ID.", zap.Error(err), zap.String("customID", customID), zap.String("username", username), zap.Bool("create", create))
			return "", "", false, status.Error(codes.Internal, "Error finding user account.")
		}
	}

	// Existing account found.
	if found {
		// Check if it's disabled.
		if dbDisableTime.Valid && dbDisableTime.Time.Unix() != 0 {
			logger.Info("User account is disabled.", zap.String("customID", customID), zap.String("username", username), zap.Bool("create", create))
			return "", "", false, status.Error(codes.PermissionDenied, "User account banned.")
		}

		return dbUserID, dbUsername, false, nil
	}

	if !create {
		// No user account found, and creation is not allowed.
		return "", "", false, status.Error(codes.NotFound, "User account not found.")
	}

	// Create a new account.
	userID := uuid.Must(uuid.NewV4()).String()
	query = "INSERT INTO users (id, username, custom_id, create_time, update_time) VALUES ($1, $2, $3, now(), now())"
	result, err := db.ExecContext(ctx, query, userID, username, customID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == dbErrorUniqueViolation {
			if strings.Contains(pgErr.Message, "users_username_key") {
				// Username is already in use by a different account.
				return "", "", false, status.Error(codes.AlreadyExists, "Username is already in use.")
			} else if strings.Contains(pgErr.Message, "users_custom_id_key") {
				// A concurrent write has inserted this custom ID.
				logger.Info("Did not insert new user as custom ID already exists.", zap.Error(err), zap.String("customID", customID), zap.String("username", username), zap.Bool("create", create))
				return "", "", false, status.Error(codes.Internal, "Error finding or creating user account.")
			}
		}
		logger.Error("Cannot find or create user with custom ID.", zap.Error(err), zap.String("customID", customID), zap.String("username", username), zap.Bool("create", create))
		return "", "", false, status.Error(codes.Internal, "Error finding or creating user account.")
	}

	if rowsAffectedCount, _ := result.RowsAffected(); rowsAffectedCount != 1 {
		logger.Error("Did not insert new user.", zap.Int64("rows_affected", rowsAffectedCount))
		return "", "", false, status.Error(codes.Internal, "Error finding or creating user account.")
	}

	return userID, username, true, nil
}

func AuthenticateDevice(ctx context.Context, logger *zap.Logger, db *sql.DB, deviceID, username string, create bool) (string, string, bool, error) {
	found := true

	// Look for an existing account.
	query := "SELECT user_id FROM user_device WHERE id = $1"
	var dbUserID string
	err := db.QueryRowContext(ctx, query, deviceID).Scan(&dbUserID)
	if err != nil {
		if err == sql.ErrNoRows {
			found = false
		} else {
			logger.Error("Error looking up user by device ID.", zap.Error(err), zap.String("deviceID", deviceID), zap.String("username", username), zap.Bool("create", create))
			return "", "", false, status.Error(codes.Internal, "Error finding user account.")
		}
	}

	// Existing account found.
	if found {
		// Load its details.
		query = "SELECT username, disable_time FROM users WHERE id = $1"
		var dbUsername string
		var dbDisableTime pgtype.Timestamptz
		err = db.QueryRowContext(ctx, query, dbUserID).Scan(&dbUsername, &dbDisableTime)
		if err != nil {
			logger.Error("Error looking up user by device ID.", zap.Error(err), zap.String("deviceID", deviceID), zap.String("username", username), zap.Bool("create", create))
			return "", "", false, status.Error(codes.Internal, "Error finding user account.")
		}

		// Check if it's disabled.
		if dbDisableTime.Valid && dbDisableTime.Time.Unix() != 0 {
			logger.Info("User account is disabled.", zap.String("deviceID", deviceID), zap.String("username", username), zap.Bool("create", create))
			return "", "", false, status.Error(codes.PermissionDenied, "User account banned.")
		}

		return dbUserID, dbUsername, false, nil
	}

	if !create {
		// No user account found, and creation is not allowed.
		return "", "", false, status.Error(codes.NotFound, "User account not found.")
	}

	// Create a new account.
	userID := uuid.Must(uuid.NewV4()).String()

	err = ExecuteInTx(ctx, db, func(tx *sql.Tx) error {
		query := `
INSERT INTO users (id, username, create_time, update_time)
SELECT $1 AS id,
		 $2 AS username,
		 now(),
		 now()
WHERE NOT EXISTS
  (SELECT id
   FROM user_device
   WHERE id = $3::VARCHAR)`

		result, err := tx.ExecContext(ctx, query, userID, username, deviceID)
		if err != nil {
			var pgErr *pgconn.PgError
			ok := errors.As(err, &pgErr)
			if err == sql.ErrNoRows || (ok && pgErr.Code == dbErrorUniqueViolation && strings.Contains(pgErr.Message, "user_device_pkey")) {
				// A concurrent write has inserted this device ID.
				logger.Info("Did not insert new user as device ID already exists.", zap.Error(err), zap.String("deviceID", deviceID), zap.String("username", username), zap.Bool("create", create))
				return StatusError(codes.Internal, "Error finding or creating user account.", err)
			} else if ok && pgErr.Code == dbErrorUniqueViolation && strings.Contains(pgErr.Message, "users_username_key") {
				return StatusError(codes.AlreadyExists, "Username is already in use.", err)
			}
			logger.Debug("Cannot find or create user with device ID.", zap.Error(err), zap.String("deviceID", deviceID), zap.String("username", username), zap.Bool("create", create))
			return err
		}

		if rowsAffectedCount, _ := result.RowsAffected(); rowsAffectedCount != 1 {
			logger.Debug("Did not insert new user.", zap.Int64("rows_affected", rowsAffectedCount))
			return StatusError(codes.Internal, "Error finding or creating user account.", ErrRowsAffectedCount)
		}

		query = "INSERT INTO user_device (id, user_id) VALUES ($1, $2)"
		result, err = tx.ExecContext(ctx, query, deviceID, userID)
		if err != nil {
			logger.Debug("Cannot add device ID.", zap.Error(err), zap.String("deviceID", deviceID), zap.String("username", username), zap.Bool("create", create))
			return err
		}

		if rowsAffectedCount, _ := result.RowsAffected(); rowsAffectedCount != 1 {
			logger.Debug("Did not insert new user.", zap.Int64("rows_affected", rowsAffectedCount))
			return StatusError(codes.Internal, "Error finding or creating user account.", ErrRowsAffectedCount)
		}

		return nil
	})
	if err != nil {
		if e, ok := err.(*statusError); ok {
			return "", "", false, e.Status()
		}
		logger.Error("Error in database transaction.", zap.Error(err))
		return "", "", false, status.Error(codes.Internal, "Error finding or creating user account.")
	}

	return userID, username, true, nil
}

func AuthenticateEmail(ctx context.Context, logger *zap.Logger, db *sql.DB, email, password, username string, create bool) (string, string, bool, error) {
	found := true

	// Look for an existing account.
	query := "SELECT id, username, password, disable_time FROM users WHERE email = $1"
	var dbUserID string
	var dbUsername string
	var dbPassword []byte
	var dbDisableTime pgtype.Timestamptz
	err := db.QueryRowContext(ctx, query, email).Scan(&dbUserID, &dbUsername, &dbPassword, &dbDisableTime)
	if err != nil {
		if err == sql.ErrNoRows {
			found = false
		} else {
			logger.Error("Error looking up user by email.", zap.Error(err), zap.String("email", email), zap.String("username", username), zap.Bool("create", create))
			return "", "", false, status.Error(codes.Internal, "Error finding user account.")
		}
	}

	// Existing account found.
	if found {
		// Check if it's disabled.
		if dbDisableTime.Valid && dbDisableTime.Time.Unix() != 0 {
			logger.Info("User account is disabled.", zap.String("email", email), zap.String("username", username), zap.Bool("create", create))
			return "", "", false, status.Error(codes.PermissionDenied, "User account banned.")
		}

		// Check if password matches.
		err = bcrypt.CompareHashAndPassword(dbPassword, []byte(password))
		if err != nil {
			return "", "", false, status.Error(codes.Unauthenticated, "Invalid credentials.")
		}

		return dbUserID, dbUsername, false, nil
	}

	if !create {
		// No user account found, and creation is not allowed.
		return "", "", false, status.Error(codes.NotFound, "User account not found.")
	}

	// Create a new account.
	userID := uuid.Must(uuid.NewV4()).String()
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcryptHashCost)
	if err != nil {
		logger.Error("Error hashing password.", zap.Error(err), zap.String("email", email), zap.String("username", username), zap.Bool("create", create))
		return "", "", false, status.Error(codes.Internal, "Error finding or creating user account.")
	}
	query = "INSERT INTO users (id, username, email, password, create_time, update_time) VALUES ($1, $2, $3, $4, now(), now())"
	result, err := db.ExecContext(ctx, query, userID, username, email, hashedPassword)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == dbErrorUniqueViolation {
			if strings.Contains(pgErr.Message, "users_username_key") {
				// Username is already in use by a different account.
				return "", "", false, status.Error(codes.AlreadyExists, "Username is already in use.")
			} else if strings.Contains(pgErr.Message, "users_email_key") {
				// A concurrent write has inserted this email.
				logger.Info("Did not insert new user as email already exists.", zap.Error(err), zap.String("email", email), zap.String("username", username), zap.Bool("create", create))
				return "", "", false, status.Error(codes.Internal, "Error finding or creating user account.")
			}
		}
		logger.Error("Cannot find or create user with email.", zap.Error(err), zap.String("email", email), zap.String("username", username), zap.Bool("create", create))
		return "", "", false, status.Error(codes.Internal, "Error finding or creating user account.")
	}

	if rowsAffectedCount, _ := result.RowsAffected(); rowsAffectedCount != 1 {
		logger.Error("Did not insert new user.", zap.Int64("rows_affected", rowsAffectedCount))
		return "", "", false, status.Error(codes.Internal, "Error finding or creating user account.")
	}

	return userID, username, true, nil
}

func AuthenticateUsername(ctx context.Context, logger *zap.Logger, db *sql.DB, username, password string) (string, error) {
	// Look for an existing account.
	query := "SELECT id, password, disable_time FROM users WHERE username = $1"
	var dbUserID string
	var dbPassword []byte
	var dbDisableTime pgtype.Timestamptz
	err := db.QueryRowContext(ctx, query, username).Scan(&dbUserID, &dbPassword, &dbDisableTime)
	if err != nil {
		if err == sql.ErrNoRows {
			// Account not found and creation is never allowed for this type.
			return "", status.Error(codes.NotFound, "User account not found.")
		}
		logger.Error("Error looking up user by username.", zap.Error(err), zap.String("username", username))
		return "", status.Error(codes.Internal, "Error finding user account.")
	}

	// Check if it's disabled.
	if dbDisableTime.Valid && dbDisableTime.Time.Unix() != 0 {
		logger.Info("User account is disabled.", zap.String("username", username))
		return "", status.Error(codes.PermissionDenied, "User account banned.")
	}

	// Check if the account has a password.
	if len(dbPassword) == 0 {
		// Do not disambiguate between bad password and password login not possible at all in client-facing error messages.
		return "", status.Error(codes.Unauthenticated, "Invalid credentials.")
	}

	// Check if password matches.
	err = bcrypt.CompareHashAndPassword(dbPassword, []byte(password))
	if err != nil {
		return "", status.Error(codes.Unauthenticated, "Invalid credentials.")
	}

	return dbUserID, nil
}

func AuthenticateApple(ctx context.Context, logger *zap.Logger, db *sql.DB, token, username string, create bool) (string, string, bool, error) {
	return "", "", false, status.Error(codes.Unimplemented, "Apple authentication is not supported.")
}

func AuthenticateFacebook(ctx context.Context, logger *zap.Logger, db *sql.DB, accessToken, username string, create bool) (string, string, bool, error) {
	return "", "", false, status.Error(codes.Unimplemented, "Facebook authentication is not supported.")
}

func AuthenticateFacebookInstantGame(ctx context.Context, logger *zap.Logger, db *sql.DB, signedPlayerInfo string, username string, create bool) (string, string, bool, error) {
	return "", "", false, status.Error(codes.Unimplemented, "Facebook Instant Game authentication is not supported.")
}

func AuthenticateGameCenter(ctx context.Context, logger *zap.Logger, db *sql.DB, playerID, bundleID string, timestamp int64, salt, signature, publicKeyUrl, username string, create bool) (string, string, bool, error) {
	return "", "", false, status.Error(codes.Unimplemented, "Game Center authentication is not supported.")
}

func RemapGoogleId(ctx context.Context, logger *zap.Logger, db *sql.DB) error {
	return errors.New("Google authentication is not supported")
}

func AuthenticateGoogle(ctx context.Context, logger *zap.Logger, db *sql.DB, idToken, username string, create bool) (string, string, bool, error) {
	return "", "", false, status.Error(codes.Unimplemented, "Google authentication is not supported.")
}

func AuthenticateSteam(ctx context.Context, logger *zap.Logger, db *sql.DB, appID int, publisherKey, token, username string, create bool) (string, string, string, bool, error) {
	return "", "", "", false, status.Error(codes.Unimplemented, "Steam authentication is not supported.")
}

func importSteamFriends(ctx context.Context, logger *zap.Logger, db *sql.DB, userID uuid.UUID, username, publisherKey, steamId string, reset bool) error {
	return status.Error(codes.Unimplemented, "Steam friends import is not supported.")
}

func importFacebookFriends(ctx context.Context, logger *zap.Logger, db *sql.DB, userID uuid.UUID, username, token string, reset bool) error {
	return status.Error(codes.Unimplemented, "Facebook friends import is not supported.")
}

func resetUserFriends(ctx context.Context, tx *sql.Tx, userID uuid.UUID) error {
	// Reset all friends for the current user, replacing them entirely with their Facebook friends.
	// Note: will NOT remove blocked users.
	query := "DELETE FROM user_edge WHERE source_id = $1 AND state != 3"
	result, err := tx.ExecContext(ctx, query, userID)
	if err != nil {
		return err
	}
	if rowsAffectedCount, _ := result.RowsAffected(); rowsAffectedCount != 0 {
		// Update edge count to reflect removed friends.
		query = "UPDATE users SET edge_count = edge_count - $2 WHERE id = $1"
		result, err := tx.ExecContext(ctx, query, userID, rowsAffectedCount)
		if err != nil {
			return err
		}
		if rowsAffectedCount, _ := result.RowsAffected(); rowsAffectedCount != 1 {
			return errors.New("error updating edge count after friends reset")
		}
	}

	// Remove links to the current user.
	// Note: will NOT remove blocks.
	query = "DELETE FROM user_edge WHERE destination_id = $1 AND state != 3 RETURNING source_id"
	rows, err := tx.QueryContext(ctx, query, userID)
	if err != nil {
		return err
	}
	params := make([]string, 0, 10)
	for rows.Next() {
		var id string
		err = rows.Scan(&id)
		if err != nil {
			_ = rows.Close()
			return err
		}
		params = append(params, id)
	}
	_ = rows.Close()

	if len(params) > 0 {
		query = "UPDATE users SET edge_count = edge_count - 1 WHERE id = ANY($1)"
		result, err := tx.ExecContext(ctx, query, params)
		if err != nil {
			return err
		}
		if rowsAffectedCount, _ := result.RowsAffected(); rowsAffectedCount != int64(len(params)) {
			return errors.New("error updating edge count after friend reset")
		}
	}

	return nil
}
