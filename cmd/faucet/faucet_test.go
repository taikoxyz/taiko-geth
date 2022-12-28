// Copyright 2021 The go-ethereum Authors
// This file is part of go-ethereum.
//
// go-ethereum is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// go-ethereum is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with go-ethereum. If not, see <http://www.gnu.org/licenses/>.

package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
)

func TestFacebook(t *testing.T) {
	// TODO: Remove facebook auth or implement facebook api, which seems to require an API key
	t.Skipf("The facebook access is flaky, needs to be reimplemented or removed")
	for _, tt := range []struct {
		url  string
		want common.Address
	}{
		{
			"https://www.facebook.com/fooz.gazonk/posts/2837228539847129",
			common.HexToAddress("0xDeadDeaDDeaDbEefbEeFbEEfBeeFBeefBeeFbEEF"),
		},
	} {
		_, _, gotAddress, err := authFacebook(tt.url)
		if err != nil {
			t.Fatal(err)
		}
		if gotAddress != tt.want {
			t.Fatalf("address wrong, have %v want %v", gotAddress, tt.want)
		}
	}
}

func Test_authTwitterWithTokenV2_StatusOK(t *testing.T) {
	testServer := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		assert.Equal(t, "/2/tweets/tweet", req.URL.Path)
		assert.Equal(t, "Bearer token", req.Header.Get("Authorization"))
		assert.Equal(t, "GET", req.Method)

		res.Header().Set("Content-Type", "application/json")
		res.WriteHeader(http.StatusOK)
		res.Write([]byte(`{
			"data": {
				"author_id": "Author",
				"text": "prefix 0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045 postfix"
			},
			"includes": {
				"users": [{
					"id": "Author",
					"username": "author",
					"profile_image_url": "https://www.example.com"
				}]
			}
		}`))
	}))
	defer testServer.Close()

	got, got1, got2, got3, err := authTwitterWithTokenV2(testServer.URL, "tweet", "token")

	assert.NoError(t, err)

	assert.Equal(t, "Author@twitter", got)
	assert.Equal(t, "author", got1)
	assert.Equal(t, "https://www.example.com", got2)
	assert.Equal(t, common.HexToAddress("0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045"), got3)
}

func Test_authTwitterWithTokenV2_StatusTooManyRequests(t *testing.T) {
	testServer := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		assert.Equal(t, "/2/tweets/tweet", req.URL.Path)
		assert.Equal(t, "Bearer token", req.Header.Get("Authorization"))
		assert.Equal(t, "GET", req.Method)

		res.Header().Set("Content-Type", "application/json")
		res.WriteHeader(http.StatusTooManyRequests)
		res.Write([]byte(`{ "errors": [ { "code": 88, "message": "Rate limit exceeded" } ] }`))
	}))
	defer testServer.Close()

	got, got1, got2, got3, err := authTwitterWithTokenV2(testServer.URL, "tweet", "token")

	assert.EqualError(t, err, "Too many requests, try again later")

	assert.Equal(t, "", got)
	assert.Equal(t, "", got1)
	assert.Equal(t, "", got2)
	assert.Equal(t, common.Address{}, got3)
}

func Test_authTwitterWithTokenV1_StatusOK(t *testing.T) {
	testServer := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		assert.Equal(t, "/1.1/statuses/show.json", req.URL.Path)
		assert.Equal(t, "Bearer token", req.Header.Get("Authorization"))
		assert.Equal(t, "GET", req.Method)

		res.Header().Set("Content-Type", "application/json")
		res.WriteHeader(http.StatusOK)
		res.Write([]byte(`{
			"text": "prefix 0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045 postfix",
			"user": {
				"id_str": "Author",
				"screen_name": "author",
				"profile_image_url": "https://www.example.com"
			}
		}`))
	}))
	defer testServer.Close()

	got, got1, got2, got3, err := authTwitterWithTokenV1(testServer.URL, "tweet", "token")

	assert.NoError(t, err)

	assert.Equal(t, "Author@twitter", got)
	assert.Equal(t, "author", got1)
	assert.Equal(t, "https://www.example.com", got2)
	assert.Equal(t, common.HexToAddress("0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045"), got3)
}

func Test_authTwitterWithTokenV1_StatusTooManyRequests(t *testing.T) {
	testServer := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		assert.Equal(t, "/1.1/statuses/show.json", req.URL.Path)
		assert.Equal(t, "Bearer token", req.Header.Get("Authorization"))
		assert.Equal(t, "GET", req.Method)

		res.Header().Set("Content-Type", "application/json")
		res.WriteHeader(http.StatusTooManyRequests)
		res.Write([]byte(`{ "errors": [ { "code": 88, "message": "Rate limit exceeded" } ] }`))
	}))
	defer testServer.Close()

	got, got1, got2, got3, err := authTwitterWithTokenV1(testServer.URL, "tweet", "token")

	assert.EqualError(t, err, "Too many requests, try again later")

	assert.Equal(t, "", got)
	assert.Equal(t, "", got1)
	assert.Equal(t, "", got2)
	assert.Equal(t, common.Address{}, got3)
}
