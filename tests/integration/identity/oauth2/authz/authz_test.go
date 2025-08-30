/*
 * Copyright (c) 2025, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

package authz

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
)

const (
	clientID      = "authz_test_client_123"
	clientSecret  = "authz_test_secret_123"
	appName       = "AuthzTestApp"
	redirectURI   = "https://localhost:3000"
	testServerURL = "https://localhost:8095"
)

type AuthzTestSuite struct {
	suite.Suite
	applicationID string
	client        *http.Client
}

func TestAuthzTestSuite(t *testing.T) {
	suite.Run(t, new(AuthzTestSuite))
}

func (ts *AuthzTestSuite) SetupSuite() {

	ts.client = &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	app := map[string]interface{}{
		"name":                         appName,
		"description":                  "Application for authorization integration tests",
		"auth_flow_graph_id":           "auth_flow_config_basic",
		"registration_flow_graph_id":   "registration_flow_config_basic",
		"is_registration_flow_enabled": true,
		"inbound_auth_config": []map[string]interface{}{
			{
				"type": "oauth2",
				"config": map[string]interface{}{
					"client_id":     clientID,
					"client_secret": clientSecret,
					"redirect_uris": []string{redirectURI},
					"grant_types": []string{
						"client_credentials",
						"authorization_code",
						"refresh_token",
					},
					"response_types": []string{
						"code",
					},
					"token_endpoint_auth_methods": []string{
						"client_secret_basic",
						"client_secret_post",
					},
				},
			},
		},
	}

	jsonData, err := json.Marshal(app)
	if err != nil {
		ts.T().Fatalf("Failed to marshal application data: %v", err)
	}

	req, err := http.NewRequest("POST", testServerURL+"/applications", bytes.NewBuffer(jsonData))
	if err != nil {
		ts.T().Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ts.client.Do(req)
	if err != nil {
		ts.T().Fatalf("Failed to create application: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		ts.T().Fatalf("Failed to create application. Status: %d, Response: %s", resp.StatusCode, string(bodyBytes))
	}

	var respData map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&respData); err != nil {
		ts.T().Fatalf("Failed to parse response: %v", err)
	}

	ts.applicationID = respData["id"].(string)
	ts.T().Logf("Created test application with ID: %s", ts.applicationID)
}

func (ts *AuthzTestSuite) TearDownSuite() {
	if ts.applicationID == "" {
		return
	}

	req, err := http.NewRequest("DELETE", fmt.Sprintf("%s/applications/%s", testServerURL, ts.applicationID), nil)
	if err != nil {
		ts.T().Errorf("Failed to create delete request: %v", err)
		return
	}

	resp, err := ts.client.Do(req)
	if err != nil {
		ts.T().Errorf("Failed to delete application: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		ts.T().Errorf("Failed to delete application. Status: %d", resp.StatusCode)
		return
	}

	ts.T().Logf("Successfully deleted test application with ID: %s", ts.applicationID)
}

// TestBasicAuthorizationRequest tests the basic authorization request flow
func (ts *AuthzTestSuite) TestBasicAuthorizationRequest() {
	testCases := []TestCase{
		{
			Name:           "Valid Request",
			ClientID:       clientID,
			RedirectURI:    redirectURI,
			ResponseType:   "code",
			Scope:          "openid",
			State:          "test_state_123",
			ExpectedStatus: http.StatusFound,
		},
		{
			Name:           "Invalid Client ID",
			ClientID:       "invalid_client",
			RedirectURI:    redirectURI,
			ResponseType:   "code",
			Scope:          "openid",
			State:          "test_state_456",
			ExpectedStatus: http.StatusFound,
			ExpectedError:  "invalid_client",
		},
		{
			Name:           "Invalid Response Type",
			ClientID:       clientID,
			RedirectURI:    redirectURI,
			ResponseType:   "invalid_type",
			Scope:          "openid",
			State:          "test_state_789",
			ExpectedStatus: http.StatusFound,
			ExpectedError:  "unsupported_response_type",
		},
		{
			Name:           "Missing Client ID",
			ClientID:       "",
			RedirectURI:    redirectURI,
			ResponseType:   "code",
			Scope:          "openid",
			State:          "test_state_missing_client",
			ExpectedStatus: http.StatusFound,
			ExpectedError:  "invalid_request",
		},
		{
			Name:           "Missing Redirect URI",
			ClientID:       clientID,
			RedirectURI:    "",
			ResponseType:   "code",
			Scope:          "openid",
			State:          "test_state_missing_redirect",
			ExpectedStatus: http.StatusFound,
		},
		{
			Name:           "Missing Response Type",
			ClientID:       clientID,
			RedirectURI:    redirectURI,
			ResponseType:   "",
			Scope:          "openid",
			State:          "test_state_missing_response",
			ExpectedStatus: http.StatusFound,
			ExpectedError:  "invalid_request",
		},
		{
			Name:           "Missing State Parameter",
			ClientID:       clientID,
			RedirectURI:    redirectURI,
			ResponseType:   "code",
			Scope:          "openid",
			State:          "",
			ExpectedStatus: http.StatusFound,
		},
	}

	for _, tc := range testCases {
		ts.Run(tc.Name, func() {
			resp, err := initiateAuthorizationFlow(testServerURL, tc.ClientID, tc.RedirectURI, tc.ResponseType, "openid", tc.State)
			ts.NoError(err, "Failed to initiate authorization flow")
			defer resp.Body.Close()

			ts.Equal(tc.ExpectedStatus, resp.StatusCode, "Expected status code")

			if tc.ExpectedStatus == http.StatusFound {
				location := resp.Header.Get("Location")
				ts.NotEmpty(location, "Expected redirect location header")
				if tc.ExpectedError != "" {
					err := validateOAuth2ErrorRedirect(location, tc.ExpectedError, "")
					ts.NoError(err, "OAuth2 error redirect validation failed")
				} else {
					sessionDataKey, _, err := extractSessionData(location)
					ts.NoError(err, "Failed to extract session data")
					ts.NotEmpty(sessionDataKey, "sessionDataKey should be present")
				}
			} else {
				bodyBytes, _ := io.ReadAll(resp.Body)
				ts.T().Logf("Error response body: %s", string(bodyBytes))
			}
		})
	}
}

// TestTokenRequestValidation tests the validation of token request parameters
func (ts *AuthzTestSuite) TestTokenRequestValidation() {
	// Create test user and get authorization code
	username := "token_test_user"
	password := "testpass123"

	userID, err := createTestUser(testServerURL, username, password)
	ts.NoError(err, "Failed to create test user")
	defer func() {
		if err := deleteTestUser(testServerURL, userID); err != nil {
			ts.T().Logf("Warning: Failed to delete test user: %v", err)
		}
	}()

	// Get a valid authorization code first
	resp, err := initiateAuthorizationFlow(testServerURL, clientID, redirectURI, "code", "openid", "token_test_state")
	ts.NoError(err, "Failed to initiate authorization flow")
	defer resp.Body.Close()

	ts.Equal(http.StatusFound, resp.StatusCode, "Expected redirect status")
	location := resp.Header.Get("Location")
	sessionDataKey, _, err := extractSessionData(location)
	ts.NoError(err, "Failed to extract session data")

	// Execute authentication flow
	flowStep, err := ExecuteAuthenticationFlow(testServerURL, ts.applicationID, map[string]string{
		"username": username,
		"password": password,
	})
	ts.NoError(err, "Failed to execute authentication flow")
	ts.Equal("COMPLETE", flowStep.FlowStatus, "Flow should complete successfully")

	// Complete authorization
	authzResponse, err := completeAuthorization(testServerURL, sessionDataKey, flowStep.Assertion)
	ts.NoError(err, "Failed to complete authorization")
	validAuthzCode, err := extractAuthorizationCode(authzResponse.RedirectURI)
	ts.NoError(err, "Failed to extract authorization code")

	testCases := []struct {
		Name           string
		ClientID       string
		ClientSecret   string
		Code           string
		RedirectURI    string
		GrantType      string
		ExpectedStatus int
		ExpectedError  string
	}{
		{
			Name:           "Missing Authorization Code",
			ClientID:       clientID,
			ClientSecret:   clientSecret,
			Code:           "",
			RedirectURI:    redirectURI,
			GrantType:      "authorization_code",
			ExpectedStatus: http.StatusBadRequest,
			ExpectedError:  "invalid_grant",
		},
		{
			Name:           "No Client ID",
			ClientID:       "",
			ClientSecret:   clientSecret,
			Code:           validAuthzCode,
			RedirectURI:    redirectURI,
			GrantType:      "authorization_code",
			ExpectedStatus: http.StatusUnauthorized,
			ExpectedError:  "invalid_client",
		},
		{
			Name:           "No Client ID and Secret",
			ClientID:       "",
			ClientSecret:   "",
			Code:           validAuthzCode,
			RedirectURI:    redirectURI,
			GrantType:      "authorization_code",
			ExpectedStatus: http.StatusUnauthorized,
			ExpectedError:  "invalid_client",
		},
		{
			Name:           "No Client Secret",
			ClientID:       clientID,
			ClientSecret:   "",
			Code:           validAuthzCode,
			RedirectURI:    redirectURI,
			GrantType:      "authorization_code",
			ExpectedStatus: http.StatusUnauthorized,
			ExpectedError:  "invalid_client",
		},
		{
			Name:           "Invalid Client Credentials",
			ClientID:       clientID,
			ClientSecret:   "wrong_secret",
			Code:           validAuthzCode,
			RedirectURI:    redirectURI,
			GrantType:      "authorization_code",
			ExpectedStatus: http.StatusUnauthorized,
			ExpectedError:  "invalid_client",
		},
		{
			Name:           "Missing Grant Type",
			ClientID:       clientID,
			ClientSecret:   clientSecret,
			Code:           validAuthzCode,
			RedirectURI:    redirectURI,
			GrantType:      "",
			ExpectedStatus: http.StatusBadRequest,
			ExpectedError:  "invalid_request",
		},
		{
			Name:           "Invalid Authorization Code",
			ClientID:       clientID,
			ClientSecret:   clientSecret,
			Code:           "invalid_code_12345",
			RedirectURI:    redirectURI,
			GrantType:      "authorization_code",
			ExpectedStatus: http.StatusBadRequest,
			ExpectedError:  "invalid_grant",
		},
		{
			Name:           "Invalid Client ID",
			ClientID:       "invalid_client_id",
			ClientSecret:   clientSecret,
			Code:           validAuthzCode,
			RedirectURI:    redirectURI,
			GrantType:      "authorization_code",
			ExpectedStatus: http.StatusUnauthorized,
			ExpectedError:  "invalid_client",
		},
		{
			Name:           "Invalid Client Secret",
			ClientID:       clientID,
			ClientSecret:   "wrong_secret",
			Code:           validAuthzCode,
			RedirectURI:    redirectURI,
			GrantType:      "authorization_code",
			ExpectedStatus: http.StatusUnauthorized,
			ExpectedError:  "invalid_client",
		},
		{
			Name:           "Mismatched Redirect URI",
			ClientID:       clientID,
			ClientSecret:   clientSecret,
			Code:           validAuthzCode,
			RedirectURI:    "https://localhost:3001",
			GrantType:      "authorization_code",
			ExpectedStatus: http.StatusBadRequest,
			ExpectedError:  "invalid_grant",
		},
		{
			Name:           "Invalid Grant Type Format",
			ClientID:       clientID,
			ClientSecret:   clientSecret,
			Code:           validAuthzCode,
			RedirectURI:    redirectURI,
			GrantType:      "invalid_grant_type",
			ExpectedStatus: http.StatusBadRequest,
			ExpectedError:  "unsupported_grant_type",
		},
		{
			Name:           "Valid Token Request",
			ClientID:       clientID,
			ClientSecret:   clientSecret,
			Code:           validAuthzCode,
			RedirectURI:    redirectURI,
			GrantType:      "authorization_code",
			ExpectedStatus: http.StatusOK,
			ExpectedError:  "",
		},
	}

	for _, tc := range testCases {
		ts.Run(tc.Name, func() {
			result, err := requestToken(testServerURL, tc.ClientID, tc.ClientSecret, tc.Code, tc.RedirectURI, tc.GrantType)
			ts.NoError(err, "Token request should not error at transport level")
			ts.Equal(tc.ExpectedStatus, result.StatusCode, "Expected status code")

			if tc.ExpectedStatus == http.StatusOK {
				ts.NotNil(result.Token, "Token payload should be present on success")

				tokenResponse := result.Token
				ts.NotEmpty(tokenResponse.AccessToken, "Access token should be present")
				ts.Equal("Bearer", tokenResponse.TokenType, "Token type should be Bearer")
				ts.True(tokenResponse.ExpiresIn > 0, "Expires in should be greater than 0")

				parts := strings.Split(tokenResponse.AccessToken, ".")
				ts.Len(parts, 3, "Access token should be a JWT with 3 parts")

				payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
				ts.NoError(err, "Failed to decode JWT payload")

				var claims map[string]interface{}
				err = json.Unmarshal(payloadBytes, &claims)
				ts.NoError(err, "Failed to unmarshal JWT claims")

				ts.Equal(tc.ClientID, claims["aud"], "Audience claim should match client_id")
				ts.Equal("openid", claims["scope"], "Scope claim should match requested scope")
				ts.Equal(userID, claims["sub"], "Subject claim should match authenticated user ID")
			} else if tc.ExpectedError != "" {
				var errorResponse map[string]interface{}
				err := json.Unmarshal(result.Body, &errorResponse)
				ts.NoError(err, "Failed to unmarshal error response")
				ts.Contains(errorResponse, "error", "Error response should contain error field")
				ts.Equal(tc.ExpectedError, errorResponse["error"], "Expected error should match")
			}
		})
	}
}

// TestRedirectURIValidation tests the redirect URI validation in OAuth2 flows
func (ts *AuthzTestSuite) TestRedirectURIValidation() {
	testCases := []struct {
		Name           string
		ClientID       string
		RedirectURI    string
		ResponseType   string
		Scope          string
		State          string
		ExpectedStatus int
		ExpectedError  string
		Description    string
	}{
		{
			Name:           "Valid HTTPS Redirect URI",
			ClientID:       clientID,
			RedirectURI:    redirectURI,
			ResponseType:   "code",
			Scope:          "openid",
			State:          "redirect_test_valid_https",
			ExpectedStatus: http.StatusFound,
			Description:    "Standard HTTPS localhost should be valid",
		},
		{
			Name:           "Valid HTTPS with Path",
			ClientID:       clientID,
			RedirectURI:    "https://localhost:3000/callback",
			ResponseType:   "code",
			Scope:          "openid",
			State:          "redirect_test_valid_path",
			ExpectedStatus: http.StatusFound,
			ExpectedError:  "invalid_request",
			Description:    "HTTPS with callback path should be rejected (not registered)",
		},
		{
			Name:           "HTTP Redirect URI",
			ClientID:       clientID,
			RedirectURI:    "http://localhost:3000",
			ResponseType:   "code",
			Scope:          "openid",
			State:          "redirect_test_http",
			ExpectedStatus: http.StatusFound,
			ExpectedError:  "invalid_request",
			Description:    "HTTP should be rejected for security",
		},
		{
			Name:           "Invalid Protocol",
			ClientID:       clientID,
			RedirectURI:    "invalid://localhost:3000",
			ResponseType:   "code",
			Scope:          "openid",
			State:          "redirect_test_invalid_protocol",
			ExpectedStatus: http.StatusFound,
			ExpectedError:  "invalid_request",
			Description:    "Invalid protocol should be rejected",
		},
		{
			Name:           "External Domain",
			ClientID:       clientID,
			RedirectURI:    "https://malicious.com/callback",
			ResponseType:   "code",
			Scope:          "openid",
			State:          "redirect_test_malicious_domain",
			ExpectedStatus: http.StatusFound,
			ExpectedError:  "invalid_request",
			Description:    "External malicious domain should be rejected",
		},
	}

	for _, tc := range testCases {
		ts.Run(tc.Name, func() {
			resp, err := initiateAuthorizationFlow(testServerURL, tc.ClientID, tc.RedirectURI, tc.ResponseType, tc.Scope, tc.State)
			ts.NoError(err, "Failed to initiate authorization flow")
			defer resp.Body.Close()

			ts.Equal(tc.ExpectedStatus, resp.StatusCode, "Expected status code")

			if tc.ExpectedStatus == http.StatusFound {
				location := resp.Header.Get("Location")
				ts.NotEmpty(location, "Expected redirect location header")

				if tc.ExpectedError != "" {
					if tc.RedirectURI != redirectURI {
						parsedLocation, parseErr := url.Parse(location)
						ts.NoError(parseErr, "Failed to parse redirect location")

						parsedTestURI, parseErr := url.Parse(tc.RedirectURI)
						ts.NoError(parseErr, "Failed to parse test case redirect URI")

						ts.NotEqual(parsedTestURI.Host, parsedLocation.Host,
							"System redirected to invalid domain '%s' instead of authorization server",
							parsedTestURI.Host)
					}

					err := validateOAuth2ErrorRedirect(location, tc.ExpectedError, "")
					ts.NoError(err, "OAuth2 error redirect validation failed")

				} else {
					sessionDataKey, _, err := extractSessionData(location)
					ts.NoError(err, "Failed to extract session data")
					ts.NotEmpty(sessionDataKey, "sessionDataKey should be present")
				}
			}
		})
	}
}

func (ts *AuthzTestSuite) TestCompleteAuthorizationCodeFlow() {
	testCases := []TestCase{
		{
			Name:         "Successful Flow",
			ClientID:     clientID,
			RedirectURI:  "https://localhost:3000",
			ResponseType: "code",
			Scope:        "openid",
			State:        "test_state_456",
			Username:     "testuser",
			Password:     "testpass123",
		},
	}

	for _, tc := range testCases {
		ts.Run(tc.Name, func() {
			// Create test user with credentials
			userID, err := createTestUser(testServerURL, tc.Username, tc.Password)
			if err != nil {
				ts.T().Fatalf("Failed to create test user: %v", err)
			}
			if userID == "" {
				ts.T().Fatalf("Expected user ID, got empty string")
			}

			defer func() {
				if err := deleteTestUser(testServerURL, userID); err != nil {
					ts.T().Logf("Warning: Failed to delete test user: %v", err)
				}
			}()

			// Start authorization flow
			resp, err := initiateAuthorizationFlow(testServerURL, tc.ClientID, tc.RedirectURI, tc.ResponseType, tc.Scope, tc.State)
			ts.NoError(err, "Failed to initiate authorization flow")
			defer resp.Body.Close()

			ts.Equal(http.StatusFound, resp.StatusCode, "Expected redirect status")

			location := resp.Header.Get("Location")
			ts.NotEmpty(location, "Expected redirect location header")

			// Extract session data
			sessionDataKey, _, err := extractSessionData(location)
			if err != nil {
				ts.T().Fatalf("Failed to extract session data: %v", err)
			}
			if sessionDataKey == "" {
				ts.T().Fatalf("Expected sessionDataKey, got empty string")
			}

			// Execute authentication flow
			flowStep, err := ExecuteAuthenticationFlow(testServerURL, ts.applicationID, map[string]string{
				"username": tc.Username,
				"password": tc.Password,
			})
			if err != nil {
				ts.T().Fatalf("Failed to execute authentication flow: %v", err)
			}
			if flowStep == nil {
				ts.T().Fatalf("Expected flow step, got nil")
			}

			if flowStep.FlowID == "" {
				ts.T().Fatalf("Expected flow ID, got empty string")
			}
			if flowStep.FlowStatus != "COMPLETE" {
				ts.T().Fatalf("Expected flow status COMPLETE, got %s", flowStep.FlowStatus)
			}

			if flowStep.Assertion == "" {
				ts.T().Fatalf("Expected assertion, got empty string")
			}

			// Complete authorization
			authzResponse, err := completeAuthorization(testServerURL, sessionDataKey, flowStep.Assertion)
			ts.NoError(err, "Failed to complete authorization")
			ts.NotEmpty(authzResponse.RedirectURI, "Redirect URI should be present")

			authzCode, err := extractAuthorizationCode(authzResponse.RedirectURI)
			ts.NoError(err, "Failed to extract authorization code")
			ts.NotEmpty(authzCode, "Authorization code should be present")

			// Exchange authorization code for access token
			result, err := requestToken(testServerURL, clientID, clientSecret, authzCode, tc.RedirectURI, "authorization_code")
			ts.NoError(err, "Failed to exchange code for token")
			ts.Equal(http.StatusOK, result.StatusCode, "Token request should succeed")
			tokenResponse := result.Token

			// Verify token response
			ts.NotEmpty(tokenResponse.AccessToken, "Access token should be present")
			ts.Equal("Bearer", tokenResponse.TokenType, "Token type should be Bearer")
			ts.True(tokenResponse.ExpiresIn > 0, "Expires in should be greater than 0")

			parts := strings.Split(tokenResponse.AccessToken, ".")
			ts.Len(parts, 3, "Access token should be a JWT with 3 parts")

			payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
			ts.NoError(err, "Failed to decode JWT payload")

			var claims map[string]interface{}
			err = json.Unmarshal(payloadBytes, &claims)
			ts.NoError(err, "Failed to unmarshal JWT claims")

			ts.Equal(tc.ClientID, claims["aud"], "Audience claim should match client_id")
			ts.Equal(tc.Scope, claims["scope"], "Scope claim should match requested scope")
			ts.Equal(userID, claims["sub"], "Subject claim should match authenticated user ID")
		})
	}
}

func (ts *AuthzTestSuite) TestAuthorizationCodeErrorScenarios() {
	testCases := []TestCase{
		{
			Name:           "Reused Authorization Code",
			ClientID:       clientID,
			RedirectURI:    "https://localhost:3000",
			ResponseType:   "code",
			Scope:          "openid",
			State:          "test_state_error",
			Username:       "testuser_error",
			Password:       "testpass123",
			ExpectedStatus: http.StatusBadRequest,
			ExpectedError:  "invalid_grant",
		}}

	for _, tc := range testCases {
		ts.Run(tc.Name, func() {
			// Create test user
			userID, err := createTestUser(testServerURL, tc.Username, tc.Password)
			ts.NoError(err, "Failed to create test user")
			defer func() {
				if err := deleteTestUser(testServerURL, userID); err != nil {
					ts.T().Logf("Warning: Failed to delete test user: %v", err)
				}
			}()

			// Start authorization flow
			resp, err := initiateAuthorizationFlow(testServerURL, tc.ClientID, tc.RedirectURI, tc.ResponseType, tc.Scope, tc.State)
			ts.NoError(err, "Failed to initiate authorization flow")
			defer resp.Body.Close()

			ts.Equal(http.StatusFound, resp.StatusCode, "Expected redirect status")

			location := resp.Header.Get("Location")
			ts.NotEmpty(location, "Expected redirect location header")

			sessionDataKey, _, err := extractSessionData(location)
			ts.NoError(err, "Failed to extract session data")

			// Execute authentication flow
			flowStep, err := ExecuteAuthenticationFlow(testServerURL, ts.applicationID, map[string]string{
				"username": tc.Username,
				"password": tc.Password,
			})
			if err != nil {
				ts.T().Fatalf("Failed to execute authentication flow: %v", err)
			}
			if flowStep.FlowStatus != "COMPLETE" {
				ts.T().Fatalf("Expected flow status COMPLETE, got %s", flowStep.FlowStatus)
			}

			authzResponse, err := completeAuthorization(testServerURL, sessionDataKey, flowStep.Assertion)
			if err != nil {
				ts.T().Fatalf("Failed to complete authorization: %v", err)
			}

			// Extract authorization code
			authzCode, err := extractAuthorizationCode(authzResponse.RedirectURI)
			ts.NoError(err, "Failed to extract authorization code")

			if tc.Name == "Reused Authorization Code" {
				result, err := requestToken(testServerURL, clientID, clientSecret, authzCode, tc.RedirectURI, "authorization_code")
				ts.NoError(err, "First token exchange should succeed")
				ts.Equal(http.StatusOK, result.StatusCode, "First token exchange should succeed")

				// Second attempt should fail with invalid_grant
				result2, err := requestToken(testServerURL, clientID, clientSecret, authzCode, tc.RedirectURI, "authorization_code")
				ts.NoError(err, "Second token exchange should not error at transport level")
				ts.Equal(http.StatusBadRequest, result2.StatusCode, "Second token exchange should fail with bad request")

				// Check error response
				var errorResponse map[string]interface{}
				err = json.Unmarshal(result2.Body, &errorResponse)
				ts.NoError(err, "Failed to unmarshal error response")
				ts.Equal("invalid_grant", errorResponse["error"], "Expected invalid_grant error")
			}
		})
	}
}
