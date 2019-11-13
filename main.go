// Copyright 2019 by David A. Golden. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jung-kurt/gofpdf"
	"github.com/shibukawa/configdir"
)

const vendorName = "xdg.me"
const appConfigName = "cardproxypdf"
const cacheDBName = "carddb.json"
const cacheImageFolder = "images"
const ringsURLGetAll = "http://ringsdb.com/api/public/cards/"
const ringsURL = "http://ringsdb.com"
const ringsImagePrefix = "/bundles/cards/"

var errIgnoreCache = errors.New("cache missing or out of date")

type App struct {
	// command line
	inputFile  string
	outputFile string

	// app-wide data
	cache *configdir.Config
	err   error

	// pipeline stage outputs
	deck           []XMLCard
	octgnToImgName map[string]string
}

type XMLCard struct {
	Quantity  int    `xml:"qty,attr"`
	OctgnID   string `xml:"id,attr"`
	ImagePath string // filled in later from metadata
}

type XMLSection struct {
	Cards []XMLCard `xml:"card"`
}

type XMLDeck struct {
	Sections []XMLSection `xml:"section"`
}

type RingsCard struct {
	ID       string `json:"octgnid"`
	ImageSrc string `json:"imagesrc"`
}

func main() {
	// Check for correct usage
	if len(os.Args) != 3 {
		log.Fatalf("usage: %s <input.o8d> <output.pdf>", os.Args[0])
	}

	app := &App{
		cache:      configdir.New(vendorName, appConfigName).QueryCacheFolder(),
		inputFile:  os.Args[1],
		outputFile: os.Args[2],
	}

	// App uses the error monad pattern; any error will shortcut later steps.
	app.LoadMetadata()
	app.ParseInputFile()
	app.PreloadImages()
	app.CreatePDF()

	if app.err != nil {
		log.Fatalf("error: %v", app.err)
	}
}

func (app *App) LoadMetadata() {
	if app.err != nil {
		return
	}

	// Try loading from cache
	var err error
	app.octgnToImgName, err = loadFromCache(app.cache)
	// Return if it worked or fall through to refetching from the API
	if err == nil {
		return
	}
	if err != errIgnoreCache {
		log.Printf("warning: failed loading metadata from cache: %v", err)
	}

	// Fetch from the API and cache the result
	log.Print("fetching metadata from ringsdb.com")
	var data []byte
	data, app.err = httpGetBytes(ringsURLGetAll)
	if app.err != nil {
		return
	}
	app.octgnToImgName, app.err = convertRingsDataToMap(data)
	if app.err != nil {
		return
	}
	err = saveToCache(app.cache, app.octgnToImgName)
	if err != nil {
		log.Printf("warning: failed saving metadata to cache: %v", err)
	}
}

func httpGetBytes(url string) ([]byte, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return body, nil
}

// store URLs as just final filename so it's easier to combine
// into full URL or cache file path
func convertRingsDataToMap(body []byte) (map[string]string, error) {
	var cardList []RingsCard
	err := json.Unmarshal(body, &cardList)
	if err != nil {
		return nil, err
	}

	images := make(map[string]string)
	for _, v := range cardList {
		images[v.ID] = strings.TrimPrefix(v.ImageSrc, ringsImagePrefix)
	}

	return images, nil
}

func loadFromCache(cache *configdir.Config) (map[string]string, error) {
	if !cache.Exists(cacheDBName) {
		return nil, errIgnoreCache
	}

	// Ignore cached file if more than 24 hours old.
	stat, err := os.Stat(filepath.Join(cache.Path, cacheDBName))
	if err != nil {
		return nil, err
	}
	if time.Since(stat.ModTime()) > 24*time.Hour {
		return nil, errIgnoreCache
	}

	// Read and unmarshal cached file
	bytes, err := cache.ReadFile(cacheDBName)
	if err != nil {
		return nil, err
	}
	var images map[string]string
	err = json.Unmarshal(bytes, &images)
	if err != nil {
		return nil, err
	}

	log.Print("loaded card metadata from cache")
	return images, nil
}

func saveToCache(cache *configdir.Config, images map[string]string) error {
	bytes, err := json.Marshal(images)
	if err != nil {
		return err
	}

	err = cache.WriteFile(cacheDBName, bytes)
	if err != nil {
		return err
	}

	log.Print("saved card metadata to cache")
	return nil
}

func (app *App) ParseInputFile() {
	if app.err != nil {
		return
	}

	var data []byte
	data, app.err = ioutil.ReadFile(app.inputFile)
	if app.err != nil {
		return
	}

	var deck XMLDeck
	app.err = xml.Unmarshal(data, &deck)
	if app.err != nil {
		return
	}

	flat := make([]XMLCard, 0)
	for _, section := range deck.Sections {
		for _, card := range section.Cards {
			card.ImagePath = app.octgnToImgName[card.OctgnID]
			flat = append(flat, card)
		}
	}

	app.deck = flat
}

func (app *App) PreloadImages() {
	if app.err != nil {
		return
	}

	wg := sync.WaitGroup{}
	var errMap sync.Map
	for _, card := range app.deck {
		if !app.cache.Exists(filepath.Join(cacheImageFolder, card.ImagePath)) {
			wg.Add(1)
			go func(card XMLCard) {
				defer wg.Done()
				err := loadImageToCache(app.cache, card.ImagePath)
				if err != nil {
					errMap.Store(card.ImagePath, err)
					return
				}
				log.Printf("Fetched %s to cache", card.ImagePath)
			}(card)
		}
	}
	wg.Wait()

	var errs []string
	errMap.Range(func(k, v interface{}) bool {
		errs = append(errs, fmt.Sprintf("%s (%s)", k.(string), v.(error).Error()))
		return true
	})
	if len(errs) > 0 {
		app.err = fmt.Errorf("error(s) fetching images: %s", strings.Join(errs, "; "))
	}
}

func loadImageToCache(cache *configdir.Config, imageName string) error {
	cachePath := filepath.Join(cacheImageFolder, imageName)
	urlPath := ringsURL + filepath.Join(ringsImagePrefix, imageName)

	imageBytes, err := httpGetBytes(urlPath)
	if err != nil {
		return err
	}

	err = cache.WriteFile(cachePath, imageBytes)
	if err != nil {
		return err
	}

	return nil
}

func (app *App) CreatePDF() {
	if app.err != nil {
		return
	}

	pdf := gofpdf.New("P", "mm", "Letter", "")

	app.err = addImagesToPdf(pdf, app.cache, app.deck)
	if app.err != nil {
		return
	}

	app.err = renderPDF(pdf, app.deck, app.outputFile)
}

func addImagesToPdf(pdf *gofpdf.Fpdf, cache *configdir.Config, deck []XMLCard) error {
	for _, card := range deck {
		imageBytes, err := cache.ReadFile(filepath.Join(cacheImageFolder, card.ImagePath))
		if err != nil {
			return err
		}
		imageOpts, err := getImageOptions(imageBytes)
		if err != nil {
			return err
		}
		pdf.RegisterImageOptionsReader(card.ImagePath, imageOpts, bytes.NewReader(imageBytes))
	}

	return nil
}

func getImageOptions(bytes []byte) (gofpdf.ImageOptions, error) {
	mimeType := http.DetectContentType(bytes)
	switch mimeType {
	case "image/jpeg":
		return gofpdf.ImageOptions{ImageType: "JPEG"}, nil
	case "image/png":
		return gofpdf.ImageOptions{ImageType: "PNG"}, nil
	default:
		return gofpdf.ImageOptions{}, fmt.Errorf("unsupported image type: %s", mimeType)
	}
}

func renderPDF(pdf *gofpdf.Fpdf, deck []XMLCard, outputPath string) error {

	images := make([]string, 0)
	for _, card := range deck {
		for i := 0; i < card.Quantity; i++ {
			images = append(images, card.ImagePath)
		}
	}

	var batch []string
	for len(images) > 0 {
		batch, images = splitAt(9, images)
		err := renderSinglePage(pdf, batch)
		if err != nil {
			return fmt.Errorf("could not assemble PDF: %v", err)
		}
	}

	err := pdf.OutputFileAndClose(outputPath)
	if err != nil {
		return fmt.Errorf("could not render PDF: %v", err)
	}

	return nil
}

func splitAt(n int, xs []string) ([]string, []string) {
	if len(xs) < n {
		n = len(xs)
	}
	return xs[0:n], xs[n:]
}

func renderSinglePage(pdf gofpdf.Pdf, images []string) error {
	if len(images) > 9 {
		return fmt.Errorf("too many images to render (%d > 9)", len(images))
	}

	pdf.AddPage()

	var cardWidth = 63.5
	var cardHeight = 88.0
	var leftPadding = 4.0
	var spacer = 4.0

	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			if len(images) == 0 {
				return nil
			}
			fi, fj := float64(i), float64(j)

			pdf.ImageOptions(
				images[0],
				leftPadding+spacer*(fj+1)+cardWidth*fj,
				spacer*(fi+1)+cardHeight*fi,
				cardWidth, cardHeight, false, gofpdf.ImageOptions{}, 0, "",
			)
			images = images[1:]
		}
	}

	return nil
}
