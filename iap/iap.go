// Copyright 2020 Heroic Labs.
// All rights reserved.
//
// NOTICE: All information contained herein is, and remains the property of Heroic
// Labs. and its suppliers, if any. The intellectual and technical concepts
// contained herein are proprietary to Heroic Labs. and its suppliers and may be
// covered by U.S. and Foreign Patents, patents in process, and are protected by
// trade secret or copyright law. Dissemination of this information or reproduction
// of this material is strictly forbidden unless prior written permission is
// obtained from Heroic Labs.

package iap

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/heroiclabs/nakama-common/runtime"
)

const (
	AppleReceiptValidationUrlSandbox    = "https://sandbox.itunes.apple.com/verifyReceipt"
	AppleReceiptValidationUrlProduction = "https://buy.itunes.apple.com/verifyReceipt"
)

const (
	AppleReceiptIsValid           = 0
	AppleReceiptIsFromTestSandbox = 21007 // Receipt from test env was sent to prod. Should retry against the sandbox env.
)

const (
	AppleSandboxEnvironment    = "Sandbox"
	AppleProductionEnvironment = "Production"
)

const accessTokenExpiresGracePeriod = 300 // 5 min grace period

var cachedTokensGoogle = &googleTokenCache{
	tokenMap: make(map[string]*accessTokenGoogle),
}

type googleTokenCache struct {
	sync.RWMutex
	tokenMap map[string]*accessTokenGoogle
}

type ValidationError struct {
	Err        error
	StatusCode int
	Payload    string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s, status=%d, payload=%s", e.Err.Error(), e.StatusCode, e.Payload)
}
func (e *ValidationError) Unwrap() error { return e.Err }

var (
	ErrNon200ServiceApple  = errors.New("non-200 response from Apple service")
	ErrNon200ServiceGoogle = errors.New("non-200 response from Google service")
)

func init() {
	// Hint to the JWT encoder that single-string arrays should be marshaled as strings.
	// This ensures that for example `["foo"]` is marshaled as `"foo"`.
	// Note: this is required particularly for Google IAP verification JWT audience fields.
	jwt.MarshalSingleStringAsArray = false
}

// Apple

type ValidateReceiptAppleResponseReceiptInApp struct {
	OriginalTransactionID string `json:"original_transaction_id"`
	TransactionId         string `json:"transaction_id"` // Different from OriginalTransactionId if the user Auto-renews subscription or restores a purchase.
	ProductID             string `json:"product_id"`
	ExpiresDateMs         string `json:"expires_date_ms"` // Subscription expiration or renewal date.
	PurchaseDateMs        string `json:"purchase_date_ms"`
	CancellationDateMs    string `json:"cancellation_date_ms"`
}

type ValidateReceiptAppleResponseReceipt struct {
	OriginalPurchaseDateMs string                                      `json:"original_purchase_date_ms"`
	InApp                  []*ValidateReceiptAppleResponseReceiptInApp `json:"in_app"`
}

type ValidateReceiptAppleResponseLatestReceiptInfo struct {
	CancellationDateMs          string `json:"cancellation_date_ms"`
	CancellationReason          string `json:"cancellation_reason"`
	ExpiresDateMs               string `json:"expires_date_ms"`
	InAppOwnershipType          string `json:"in_app_ownership_type"`
	IsInIntroOfferPeriod        string `json:"is_in_intro_offer_period"` // "true" or "false"
	IsTrialPeriod               string `json:"is_trial_period"`
	IsUpgraded                  string `json:"is_upgraded"`
	OfferCodeRefName            string `json:"offer_code_ref_name"`
	OriginalPurchaseDateMs      string `json:"original_purchase_date_ms"`
	OriginalTransactionId       string `json:"original_transaction_id"` // First subscription transaction
	ProductId                   string `json:"product_id"`
	PromotionalOfferId          string `json:"promotional_offer_id"`
	PurchaseDateMs              string `json:"purchase_date_ms"`
	Quantity                    string `json:"quantity"`
	SubscriptionGroupIdentifier string `json:"subscription_group_identifier"`
	TransactionId               string `json:"transaction_id"` // Different from OriginalTransactionId if the user Auto-renews subscription or restores a purchase.
}

type ValidateReceiptAppleResponsePendingRenewalInfo struct {
	AutoRenewProductId       string `json:"auto_renew_product_id"`
	AutoRenewStatus          string `json:"auto_renew_status"` // 1: subscription will renew at end of current subscription period, 0: the customer has turned off automatic renewal for the subscription.
	ExpirationIntent         string `json:"expiration_intent"`
	GracePeriodExpiresDateMs string `json:"grace_period_expires_date_ms"`
	IsInBillingRetryPeriod   string `json:"is_in_billing_retry_period"`
	OfferCodeRefName         string `json:"offer_code_ref_name"`
	OriginalTransactionId    string `json:"original_transaction_id"`
	PriceConsentStatus       string `json:"price_consent_status"`
	ProductId                string `json:"product_id"`
	PromotionalOfferId       string `json:"promotional_offer_id"`
}

type ValidateReceiptAppleResponse struct {
	Environment        string                                           `json:"environment"`  // possible values: 'Sandbox', 'Production'.
	IsRetryable        bool                                             `json:"is-retryable"` // If true, request must be retried later.
	LatestReceipt      string                                           `json:"latest_receipt"`
	LatestReceiptInfo  []ValidateReceiptAppleResponseLatestReceiptInfo  `json:"latest_receipt_info"`
	PendingRenewalInfo []ValidateReceiptAppleResponsePendingRenewalInfo `json:"pending_renewal_info"` // Only returned for auto-renewable subscriptions.
	Receipt            *ValidateReceiptAppleResponseReceipt             `json:"receipt"`
	Status             int                                              `json:"status"`
}

// Validate an IAP receipt with Apple. This function will check against both the production and sandbox Apple URLs.
func ValidateReceiptApple(ctx context.Context, httpc *http.Client, receipt, password string) (*ValidateReceiptAppleResponse, []byte, error) {
	resp, raw, err := ValidateReceiptAppleWithUrl(ctx, httpc, AppleReceiptValidationUrlProduction, receipt, password)
	if err != nil {
		return nil, nil, err
	}

	switch resp.Status {
	case AppleReceiptIsFromTestSandbox:
		// Receipt should be checked with the Apple sandbox.
		return ValidateReceiptAppleWithUrl(ctx, httpc, AppleReceiptValidationUrlSandbox, receipt, password)
	}

	return resp, raw, nil
}

// Validate an IAP receipt with Apple against the specified URL.
func ValidateReceiptAppleWithUrl(ctx context.Context, httpc *http.Client, url, receipt, password string) (*ValidateReceiptAppleResponse, []byte, error) {
	if len(url) < 1 {
		return nil, nil, errors.New("'url' must not be empty")
	}

	if len(receipt) < 1 {
		return nil, nil, errors.New("'receipt' must not be empty")
	}

	if len(password) < 1 {
		return nil, nil, errors.New("'password' must not be empty")
	}

	payload := map[string]interface{}{
		"receipt-data":             receipt,
		"exclude-old-transactions": true,
		"password":                 password,
	}

	var w bytes.Buffer
	if err := json.NewEncoder(&w).Encode(&payload); err != nil {
		return nil, nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &w)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := httpc.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}

	switch resp.StatusCode {
	case 200:
		var out ValidateReceiptAppleResponse
		if err := json.Unmarshal(buf, &out); err != nil {
			return nil, nil, err
		}

		// Sort by ExpiresDateMs in desc order
		sort.Slice(out.LatestReceiptInfo, func(i, j int) bool {
			return sort.StringsAreSorted([]string{out.LatestReceiptInfo[j].ExpiresDateMs, out.LatestReceiptInfo[i].ExpiresDateMs})
		})

		return &out, buf, nil
	default:
		return nil, nil, &ValidationError{
			Err:        ErrNon200ServiceApple,
			StatusCode: resp.StatusCode,
			Payload:    string(buf),
		}
	}
}

// Google

type ReceiptGoogle struct {
	OrderID       string `json:"orderId"`
	PackageName   string `json:"packageName"`
	ProductID     string `json:"productId"`
	PurchaseState int    `json:"purchaseState"`
	PurchaseTime  int64  `json:"purchaseTime"`
	PurchaseToken string `json:"purchaseToken"`
}

// https://developers.google.com/android-publisher/api-ref/rest/v3/purchases.products#ProductPurchase
type ValidateReceiptGoogleResponse struct {
	AcknowledgementState int    `json:"acknowledgementState"`
	ConsumptionState     int    `json:"consumptionState"`
	DeveloperPayload     string `json:"developerPayload"`
	Kind                 string `json:"kind"`
	OrderId              string `json:"orderId"`
	PurchaseState        int    `json:"purchaseState"` //Possible values are: 0. Purchased 1. Canceled 2. Pending
	PurchaseTimeMillis   string `json:"purchaseTimeMillis"`
	PurchaseType         int    `json:"purchaseType"`
	RegionCode           string `json:"regionCode"`
}

// A helper function to unwrap a receipt response from the Android Publisher API.
//
// The standard structure looks like:
//
//	"{\"json\":\"{\\\"orderId\\\":\\\"..\\\",\\\"packageName\\\":\\\"..\\\",\\\"productId\\\":\\\"..\\\",
//	    \\\"purchaseTime\\\":1607721533824,\\\"purchaseState\\\":0,\\\"purchaseToken\\\":\\\"..\\\",
//	    \\\"acknowledged\\\":false}\",\"signature\":\"..\",\"skuDetails\":\"{\\\"productId\\\":\\\"..\\\",
//	    \\\"type\\\":\\\"inapp\\\",\\\"price\\\":\\\"\\u20ac82.67\\\",\\\"price_amount_micros\\\":82672732,
//	    \\\"price_currency_code\\\":\\\"EUR\\\",\\\"title\\\":\\\"..\\\",\\\"description\\\":\\\"..\\\",
//	    \\\"skuDetailsToken\\\":\\\"..\\\"}\"}"
func decodeReceiptGoogle(receipt string) (*ReceiptGoogle, error) {
	var wrapper map[string]interface{}
	if err := json.Unmarshal([]byte(receipt), &wrapper); err != nil {
		return nil, err
	}

	unwrapped, ok := wrapper["json"].(string)
	if !ok {
		// If there is no 'json' field, assume the receipt is not in a
		// wrapper. Just attempt and decode from the top level instead.
		unwrapped = receipt
	}

	var gr ReceiptGoogle
	if err := json.Unmarshal([]byte(unwrapped), &gr); err != nil {
		return nil, errors.New("receipt is malformed")
	}
	if gr.PackageName == "" {
		return nil, errors.New("receipt is malformed")
	}

	return &gr, nil
}

type accessTokenGoogle struct {
	AccessToken  string    `json:"access_token"`
	ExpiresIn    int       `json:"expires_in"` // Seconds
	TokenType    string    `json:"token_type"`
	RefreshToken string    `json:"refresh_token"`
	Scope        string    `json:"scope"`
	fetchedAt    time.Time // Set when token is received
}

func (at *accessTokenGoogle) Expired() bool {
	return at.fetchedAt.Add(time.Duration(at.ExpiresIn)*time.Second - accessTokenExpiresGracePeriod*time.Second).Before(time.Now())
}

// Request an authenticated context (token) from Google for the Android publisher service.
// https://developers.google.com/identity/protocols/oauth2#serviceaccount
func getGoogleAccessToken(ctx context.Context, httpc *http.Client, email, privateKey string) (string, error) {
	const authUrl = "https://accounts.google.com/o/oauth2/token"

	cachedTokensGoogle.RLock()
	cacheToken, found := cachedTokensGoogle.tokenMap[email]
	if found && cacheToken.AccessToken != "" && !cacheToken.Expired() {
		cachedTokensGoogle.RUnlock()
		return cacheToken.AccessToken, nil
	}
	cachedTokensGoogle.RUnlock()

	cachedTokensGoogle.Lock()
	cacheToken, found = cachedTokensGoogle.tokenMap[email]
	if found && cacheToken.AccessToken != "" && !cacheToken.Expired() {
		cachedTokensGoogle.Unlock()
		return cacheToken.AccessToken, nil
	}
	defer cachedTokensGoogle.Unlock()

	type GoogleClaims struct {
		Scope string `json:"scope,omitempty"`
		jwt.RegisteredClaims
	}

	now := time.Now()
	claims := &GoogleClaims{
		"https://www.googleapis.com/auth/androidpublisher",
		jwt.RegisteredClaims{
			Audience:  jwt.ClaimStrings{authUrl},
			ExpiresAt: jwt.NewNumericDate(now.Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    email,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	block, _ := pem.Decode([]byte(privateKey))
	if block == nil {
		return "", errors.New("google iap private key invalid")
	}

	pk, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", err
	}
	signed, err := token.SignedString(pk)
	if err != nil {
		return "", err
	}

	data := url.Values{}
	data.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	data.Set("assertion", signed)
	body := data.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, authUrl, strings.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := httpc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	switch resp.StatusCode {
	case 200:
		newToken := accessTokenGoogle{}
		if err := json.Unmarshal(buf, &newToken); err != nil {
			return "", err
		}
		newToken.fetchedAt = time.Now()
		cachedTokensGoogle.tokenMap[email] = &newToken
		return newToken.AccessToken, nil
	default:
		return "", &ValidationError{
			Err:        errors.New("non-200 response from Google auth"),
			StatusCode: resp.StatusCode,
			Payload:    string(buf),
		}
	}
}

// Validate an IAP receipt with the Android Publisher API and the Google credentials.
func ValidateReceiptGoogle(ctx context.Context, httpc *http.Client, clientEmail, privateKey, receipt string) (*ValidateReceiptGoogleResponse, *ReceiptGoogle, []byte, error) {
	if len(clientEmail) < 1 {
		return nil, nil, nil, errors.New("'clientEmail' must not be empty")
	}

	if len(privateKey) < 1 {
		return nil, nil, nil, errors.New("'privateKey' must not be empty")
	}

	if len(receipt) < 1 {
		return nil, nil, nil, errors.New("'receipt' must not be empty")
	}

	token, err := getGoogleAccessToken(ctx, httpc, clientEmail, privateKey)
	if err != nil {
		return nil, nil, nil, err
	}

	return validateReceiptGoogleWithIDs(ctx, httpc, token, receipt)
}

// Validate an IAP receipt with the Android Publisher API using a Google token.
func validateReceiptGoogleWithIDs(ctx context.Context, httpc *http.Client, token, receipt string) (*ValidateReceiptGoogleResponse, *ReceiptGoogle, []byte, error) {
	if len(token) < 1 {
		return nil, nil, nil, errors.New("'token' must not be empty")
	}

	if len(receipt) < 1 {
		return nil, nil, nil, errors.New("'receipt' must not be empty")
	}

	gr, err := decodeReceiptGoogle(receipt)
	if err != nil {
		return nil, nil, nil, err
	}

	u := &url.URL{
		Host:     "androidpublisher.googleapis.com",
		Path:     fmt.Sprintf("androidpublisher/v3/applications/%s/purchases/products/%s/tokens/%s", gr.PackageName, gr.ProductID, gr.PurchaseToken),
		RawQuery: fmt.Sprintf("access_token=%s", token),
		Scheme:   "https",
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := httpc.Do(req)
	if err != nil {
		return nil, nil, nil, err
	}

	defer resp.Body.Close()

	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, nil, err
	}

	switch resp.StatusCode {
	case 200:
		out := &ValidateReceiptGoogleResponse{}
		out.PurchaseType = -1 // Set sentinel value as this field is omitted in production, and if set to 0 it means the purchase was done in sandbox env.
		if err := json.Unmarshal(buf, &out); err != nil {
			return nil, nil, nil, err
		}

		return out, gr, buf, nil
	default:
		return nil, nil, nil, &ValidationError{
			Err:        ErrNon200ServiceGoogle,
			StatusCode: resp.StatusCode,
			Payload:    string(buf),
		}
	}
}

// https://developers.google.com/android-publisher/api-ref/rest/v3/purchases.subscriptions#get
type ValidateSubscriptionReceiptGoogleResponse struct {
	Kind                        string `json:"kind"`
	StartTimeMillis             string `json:"startTimeMillis"`
	ExpiryTimeMillis            string `json:"expiryTimeMillis"`
	AutoResumeTimeMillis        string `json:"autoResumeTimeMillis"`
	AutoRenewing                bool   `json:"autoRenewing"`
	PriceCurrencyCode           string `json:"priceCurrencyCode"`
	PriceAmountMicros           string `json:"priceAmountMicros"`
	CountryCode                 string `json:"countryCode"`
	DeveloperPayload            string `json:"developerPayload"`
	PaymentState                int    `json:"paymentState"`
	CancelReason                int    `json:"cancelReason"`
	UserCancellationTimeMillis  string `json:"userCancellationTimeMillis"`
	OrderId                     string `json:"orderId"`
	LinkedPurchaseToken         string `json:"linkedPurchaseToken"`
	PurchaseType                int    `json:"purchaseType"`
	ProfileName                 string `json:"profileName"`
	EmailAddress                string `json:"emailAddress"`
	GivenName                   string `json:"givenName"`
	FamilyName                  string `json:"familyName"`
	ProfileId                   string `json:"profileId"`
	AcknowledgementState        int    `json:"acknowledgementState"`
	ExternalAccountId           string `json:"externalAccountId"`
	PromotionType               int    `json:"promotionType"`
	PromotionCode               string `json:"promotionCode"`
	ObfuscatedExternalAccountId string `json:"obfuscatedExternalAccountId"`
	ObfuscatedExternalProfileId string `json:"obfuscatedExternalProfileId"`
}

// Validate an IAP Subscription receipt with the Android Publisher API and the Google credentials.
func ValidateSubscriptionReceiptGoogle(ctx context.Context, httpc *http.Client, clientEmail string, privateKey string, receipt string) (*runtime.SubscriptionV2GoogleResponse, *ReceiptGoogle, []byte, error) {
	if len(clientEmail) < 1 {
		return nil, nil, nil, errors.New("'clientEmail' must not be empty")
	}

	if len(privateKey) < 1 {
		return nil, nil, nil, errors.New("'privateKey' must not be empty")
	}

	if len(receipt) < 1 {
		return nil, nil, nil, errors.New("'receipt' must not be empty")
	}

	gr, err := decodeReceiptGoogle(receipt)
	if err != nil {
		return nil, nil, nil, err
	}

	response, buf, err := GetSubscriptionV2Google(ctx, httpc, clientEmail, privateKey, gr.PackageName, gr.PurchaseToken)
	if err != nil {
		return nil, nil, nil, err
	}

	return response, gr, buf, nil
}

func GetPurchaseV2Google(ctx context.Context, httpc *http.Client, clientEmail, privateKey, packageName, purchaseToken string) (*runtime.PurchaseV2GoogleResponse, error) {
	if len(clientEmail) < 1 {
		return nil, errors.New("'clientEmail' must not be empty")
	}

	if len(privateKey) < 1 {
		return nil, errors.New("'privateKey' must not be empty")
	}

	if len(purchaseToken) < 1 {
		return nil, errors.New("'purchaseToken' must not be empty")
	}

	token, err := getGoogleAccessToken(ctx, httpc, clientEmail, privateKey)
	if err != nil {
		return nil, err
	}

	u := &url.URL{
		Host:     "androidpublisher.googleapis.com",
		Path:     fmt.Sprintf("androidpublisher/v3/applications/%s/purchases/productsv2/tokens/%s", packageName, purchaseToken),
		RawQuery: fmt.Sprintf("access_token=%s", token),
		Scheme:   "https",
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := httpc.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	switch resp.StatusCode {
	case 200:
		out := &runtime.PurchaseV2GoogleResponse{}
		if err = json.Unmarshal(buf, &out); err != nil {
			return nil, err
		}

		return out, nil
	default:
		return nil, &ValidationError{
			Err:        ErrNon200ServiceGoogle,
			StatusCode: resp.StatusCode,
			Payload:    string(buf),
		}
	}
}

func GetSubscriptionV2Google(ctx context.Context, httpc *http.Client, clientEmail, privateKey, packageName, purchaseToken string) (*runtime.SubscriptionV2GoogleResponse, []byte, error) {
	if len(clientEmail) < 1 {
		return nil, nil, errors.New("'clientEmail' must not be empty")
	}

	if len(privateKey) < 1 {
		return nil, nil, errors.New("'privateKey' must not be empty")
	}

	if len(purchaseToken) < 1 {
		return nil, nil, errors.New("'purchaseToken' must not be empty")
	}

	token, err := getGoogleAccessToken(ctx, httpc, clientEmail, privateKey)
	if err != nil {
		return nil, nil, err
	}

	u := &url.URL{
		Host:     "androidpublisher.googleapis.com",
		Path:     fmt.Sprintf("androidpublisher/v3/applications/%s/purchases/subscriptionsv2/tokens/%s", packageName, purchaseToken),
		RawQuery: fmt.Sprintf("access_token=%s", token),
		Scheme:   "https",
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := httpc.Do(req)
	if err != nil {
		return nil, nil, err
	}

	defer resp.Body.Close()

	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}

	switch resp.StatusCode {
	case 200:
		out := &runtime.SubscriptionV2GoogleResponse{}
		if err = json.Unmarshal(buf, &out); err != nil {
			return nil, nil, err
		}

		return out, nil, nil
	default:
		return nil, nil, &ValidationError{
			Err:        ErrNon200ServiceGoogle,
			StatusCode: resp.StatusCode,
			Payload:    string(buf),
		}
	}
}
