// genmap builds web/world.json: country outlines as SVG paths and one marker
// position per country code, both already projected, so the browser draws the
// map without a mapping library or a tile server.
//
// Usage (from the repository root):
//
//	go run ./tools/genmap -shapes ne_110m_admin_0_countries.geojson \
//	    -labels ne_50m_admin_0_countries.geojson -o web/world.json
//
// Both inputs are Natural Earth (public domain), from
// github.com/nvkelso/natural-earth-vector/tree/master/geojson. Outlines come
// from the coarse 1:110m set to stay small; marker positions come from the
// 1:50m set's LABEL_X/LABEL_Y, because 1:110m leaves out small countries that
// host plenty of Bitcoin nodes (Singapore, Hong Kong, Malta, ...). The few
// codes DB-IP uses that 1:50m has no entry for are filled in by hand below.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strings"
)

const width = 1000.0

// Territories DB-IP reports that Natural Earth 1:50m does not list separately.
// Approximate position of the main settlement, [lon, lat].
var extra = map[string][2]float64{
	"BQ": {-68.27, 12.15},  // Bonaire (Kralendijk)
	"BV": {3.36, -54.42},   // Bouvet Island
	"CC": {96.83, -12.19},  // Cocos (Keeling) Islands
	"CX": {105.62, -10.49}, // Christmas Island
	"GF": {-52.33, 4.93},   // French Guiana (Cayenne)
	"GI": {-5.35, 36.14},   // Gibraltar
	"GP": {-61.58, 16.25},  // Guadeloupe
	"MQ": {-61.02, 14.64},  // Martinique
	"RE": {55.53, -21.12},  // Reunion
	"SJ": {15.65, 78.22},   // Svalbard (Longyearbyen)
	"TK": {-171.82, -9.17}, // Tokelau
	"UM": {166.65, 19.28},  // US Minor Outlying Islands (Wake Island)
	"YT": {45.14, -12.83},  // Mayotte
}

type fc struct {
	Features []struct {
		Properties map[string]any `json:"properties"`
		Geometry   struct {
			Type        string          `json:"type"`
			Coordinates json.RawMessage `json:"coordinates"`
		} `json:"geometry"`
	} `json:"features"`
}

// Natural Earth projection (Savric, Jenny, Patterson, Petrovic, Hurni 2011),
// same polynomial as d3.geoNaturalEarth1.
func project(lon, lat float64) (float64, float64) {
	l, p := lon*math.Pi/180, lat*math.Pi/180
	p2 := p * p
	p4 := p2 * p2
	x := l * (0.8707 - 0.131979*p2 + p4*(-0.013791+p4*(0.003971*p2-0.001529*p4)))
	y := p * (1.007226 + p2*(0.015085+p4*(-0.044475+0.028874*p2-0.005916*p4)))
	return x, y
}

var xMax, yMax, yMin float64

func init() {
	xMax, _ = project(180, 0)
	_, yMax = project(0, 84)  // northernmost land drawn
	_, yMin = project(0, -58) // Antarctica is left out
}

func px(lon, lat float64) (float64, float64) {
	x, y := project(lon, lat)
	s := width / (2 * xMax)
	return (x + xMax) * s, (yMax - y) * s
}

func height() float64 { return (yMax - yMin) * width / (2 * xMax) }

func code(p map[string]any) string {
	for _, k := range []string{"ISO_A2_EH", "ISO_A2", "WB_A2"} {
		if v, ok := p[k].(string); ok && len(v) == 2 && v != "-9" {
			return v
		}
	}
	return ""
}

func load(path string) fc {
	b, err := os.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}
	var f fc
	if err := json.Unmarshal(b, &f); err != nil {
		log.Fatal(err)
	}
	return f
}

func ring(sb *strings.Builder, pts [][]float64) {
	lastX, lastY := math.NaN(), math.NaN()
	n := 0
	for i, pt := range pts {
		x, y := px(pt[0], pt[1])
		x, y = math.Round(x*10)/10, math.Round(y*10)/10
		if x == lastX && y == lastY {
			continue
		}
		if i == 0 {
			fmt.Fprintf(sb, "M%g %g", x, y)
		} else {
			fmt.Fprintf(sb, "L%g %g", x, y)
		}
		lastX, lastY = x, y
		n++
	}
	if n > 0 {
		sb.WriteString("Z")
	}
}

func main() {
	shapes := flag.String("shapes", "", "ne_110m_admin_0_countries.geojson")
	labels := flag.String("labels", "", "ne_50m_admin_0_countries.geojson")
	out := flag.String("o", "web/world.json", "output file")
	flag.Parse()
	if *shapes == "" || *labels == "" {
		log.Fatal("-shapes and -labels are required")
	}

	paths := map[string]string{}
	var unnamed []string
	for _, ft := range load(*shapes).Features {
		cc := code(ft.Properties)
		if cc == "AQ" {
			continue
		}
		var sb strings.Builder
		switch ft.Geometry.Type {
		case "Polygon":
			var poly [][][]float64
			json.Unmarshal(ft.Geometry.Coordinates, &poly)
			for _, r := range poly {
				ring(&sb, r)
			}
		case "MultiPolygon":
			var mp [][][][]float64
			json.Unmarshal(ft.Geometry.Coordinates, &mp)
			for _, poly := range mp {
				for _, r := range poly {
					ring(&sb, r)
				}
			}
		}
		if cc == "" {
			unnamed = append(unnamed, sb.String())
		} else {
			paths[cc] += sb.String()
		}
	}

	// Several features can share a code: in 1:50m, "AU" is Australia and also
	// the Indian Ocean Territories and Ashmore and Cartier Islands. The
	// marker belongs on the one people live in, so the most populous wins.
	points := map[string][2]float64{}
	pop := map[string]float64{}
	for _, ft := range load(*labels).Features {
		cc := code(ft.Properties)
		lx, ok1 := ft.Properties["LABEL_X"].(float64)
		ly, ok2 := ft.Properties["LABEL_Y"].(float64)
		if cc == "" || !ok1 || !ok2 {
			continue
		}
		pe, _ := ft.Properties["POP_EST"].(float64)
		if prev, seen := pop[cc]; seen && prev >= pe {
			continue
		}
		pop[cc] = pe
		x, y := px(lx, ly)
		points[cc] = [2]float64{math.Round(x*10) / 10, math.Round(y*10) / 10}
	}
	for cc, ll := range extra {
		if _, ok := points[cc]; !ok {
			x, y := px(ll[0], ll[1])
			points[cc] = [2]float64{math.Round(x*10) / 10, math.Round(y*10) / 10}
		}
	}

	doc := map[string]any{
		"w":       width,
		"h":       math.Round(height()),
		"paths":   paths,
		"unnamed": strings.Join(unnamed, ""),
		"points":  points,
	}
	b, _ := json.Marshal(doc)
	if err := os.WriteFile(*out, b, 0o644); err != nil {
		log.Fatal(err)
	}
	keys := make([]string, 0, len(points))
	for k := range points {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Printf("outlines=%d unnamed=%d points=%d height=%.0f -> %s (%d bytes)\n", len(paths), len(unnamed), len(points), height(), *out, len(b))
}
