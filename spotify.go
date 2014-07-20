package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
)

type Spotify struct {
	AccessToken  string      `json:"access_token"`
	TokenType    string      `json:"token_type"`
	ExpiresIn    uint        `json:"expires_in"`
	RefreshToken string      `json:"refresh_token"`
	Auth         SpotifyAuth `json:"auth"`
}

type SpotifyProfile struct {
	ExternalUrls map[string]string `json:"external_urls"`
	Href         string            `json:"href"`
	Id           string            `json:"id"`
	Type         string            `json:"type"`
	Uri          string            `json:"uri"`
}

type Playlist struct {
	Id   string `json:"id"`
	Name string `json:"name"`
}

type Playlists struct {
	Items []Playlist `json:"items"`
}

type NewPlaylist struct {
	Name   string `json:"name"`
	Public bool   `json:"public"`
}

type SearchResult struct {
	Tracks Tracks `json:"tracks"`
}

type Tracks struct {
	Items []Track `json:"items"`
}

type Track struct {
	Id   string `json:"id"`
	Name string `json:"name"`
	Uri  string `json:"uri"`
}

func (spotify *Spotify) update(newToken *Spotify) {
	spotify.AccessToken = newToken.AccessToken
	spotify.TokenType = newToken.TokenType
	spotify.ExpiresIn = newToken.ExpiresIn
}

func (spotify *Spotify) refreshToken() error {
	formData := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {spotify.RefreshToken},
	}
	url := "https://accounts.spotify.com/api/token"
	client := &http.Client{}
	req, err := http.NewRequest("POST", url,
		bytes.NewBufferString(formData.Encode()))
	req.Header.Set("Authorization", spotify.Auth.authHeader())
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var newToken Spotify
	if err := json.Unmarshal(body, &newToken); err != nil {
		return err
	}
	spotify.update(&newToken)
	return nil
}

func (spotify *Spotify) authHeader() string {
	return spotify.TokenType + " " + spotify.AccessToken
}

func (spotify *Spotify) doGet(url string) (*http.Response, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", spotify.authHeader())
	if err != nil {
		return nil, err
	}
	return client.Do(req)
}

func (spotify *Spotify) get(url string) ([]byte, error) {
	resp, err := spotify.doGet(url)
	if err != nil {
		return nil, err
	}
	// Check if we need to refresh token
	if resp.StatusCode == 401 {
		if err := spotify.refreshToken(); err != nil {
			return nil, err
		}
		if err := spotify.save(spotify.Auth.TokenFile); err != nil {
			return nil, err
		}
		resp, err = spotify.doGet(url)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return body, err
}

func (spotify *Spotify) doPost(url string, body []byte) (*http.Response,
	error) {
	client := &http.Client{}
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	req.Header.Set("Authorization", spotify.authHeader())
	req.Header.Set("Content-Type", "application/json")
	if err != nil {
		return nil, err
	}
	return client.Do(req)
}

func (spotify *Spotify) post(url string, body []byte) ([]byte, error) {
	resp, err := spotify.doPost(url, body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == 401 {
		if err := spotify.refreshToken(); err != nil {
			return nil, err
		}
		if err := spotify.save(spotify.Auth.TokenFile); err != nil {
			return nil, err
		}
		resp, err = spotify.doPost(url, body)
	}
	defer resp.Body.Close()
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return data, err
}

func (spotify *Spotify) save(filepath string) error {
	json, err := json.Marshal(spotify)
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(filepath, json, 0600)
	if err != nil {
		return err
	}
	return nil
}

func ReadToken(filepath string) (*Spotify, error) {
	data, err := ioutil.ReadFile(filepath)
	if err != nil {
		return nil, err
	}
	var spotify Spotify
	if err := json.Unmarshal(data, &spotify); err != nil {
		return nil, err
	}
	return &spotify, nil
}

func (spotify *Spotify) currentUser() (*SpotifyProfile, error) {
	url := "https://api.spotify.com/v1/me"
	body, err := spotify.get(url)
	if err != nil {
		return nil, err
	}
	var profile SpotifyProfile
	if err := json.Unmarshal(body, &profile); err != nil {
		return nil, err
	}
	return &profile, nil
}

func (spotify *Spotify) playlists(profile *SpotifyProfile) (*Playlists, error) {
	url := fmt.Sprintf("https://api.spotify.com/v1/users/%s/playlists",
		profile.Id)
	body, err := spotify.get(url)
	if err != nil {
		return nil, err
	}
	var playlists Playlists
	if err := json.Unmarshal(body, &playlists); err != nil {
		return nil, err
	}
	return &playlists, nil
}

func (spotify *Spotify) playlist(profile *SpotifyProfile,
	name string) (*Playlist, error) {
	playlists, err := spotify.playlists(profile)
	if err != nil {
		return nil, err
	}
	for _, playlist := range playlists.Items {
		if playlist.Name == name {
			return &playlist, nil
		}
	}
	return nil, fmt.Errorf("Could not find playlist by name: %s", name)
}

func (spotify *Spotify) createPlaylist(profile *SpotifyProfile,
	name string) (*Playlist, error) {
	playlists, err := spotify.playlists(profile)
	if err != nil {
		return nil, err
	}
	for _, playlist := range playlists.Items {
		if playlist.Name == name {
			return nil, fmt.Errorf(
				"Playlist with name '%s' already exists", name)
		}
	}
	url := fmt.Sprintf("https://api.spotify.com/v1/users/%s/playlists",
		profile.Id)
	newPlaylist, err := json.Marshal(NewPlaylist{
		Name:   name,
		Public: false,
	})
	if err != nil {
		return nil, err
	}
	body, err := spotify.post(url, newPlaylist)
	if err != nil {
		return nil, err
	}
	var playlist Playlist
	if err := json.Unmarshal(body, &playlist); err != nil {
		return nil, err
	}
	return &playlist, err
}

func (spotify *Spotify) search(query string, types string, limit uint) ([]Track,
	error) {
	params := url.Values{
		"q":     {query},
		"type":  {types},
		"limit": {strconv.Itoa(int(limit))},
	}
	url := "https://api.spotify.com/v1/search?" + params.Encode()
	body, err := spotify.get(url)
	if err != nil {
		return nil, err
	}
	var result SearchResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result.Tracks.Items, nil
}

func (spotify *Spotify) searchArtistTrack(artist string, track string) ([]Track,
	error) {
	query := fmt.Sprintf("artist:%s track:%s", artist, track)
	return spotify.search(query, "track", 1)
}