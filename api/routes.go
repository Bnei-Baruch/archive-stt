// Package api: the HTTP API (net/http).
//
//	POST /cus/{uid}/markup?language=&force=false   a first run -> 202 + the run   (X-API-Key if keys are set, else from this host only)
//	POST /cus/{uid}/rebuild?run=                   a new revision auto from a run's kept words and diarization
//	                                               and the transcript now, no ai-trns -> 201 (200 if unchanged)
//	GET  /cus/{uid}/markup?rev=                    the markup document, public; 404 until a run is ready
//	GET  /cus/{uid}/timecode?lang=                 the time of every word of the transcript: paragraphs[p][w] --
//	                                               the w-th word (by whitespace) of the p-th <p> of its doc2html;
//	                                               public; 404 without markup on a transcript
//	GET  /cus/{uid}/runs                           the lesson's runs, newest first, public
//	GET  /cus/{uid}/runs/latest | /runs/{id}       one run, public
//	GET  /health
package api

import "net/http"

// Handler is the API with its middleware
func Handler(h *Handlers) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", h.Health)
	mux.HandleFunc("POST /cus/{uid}/markup", h.StartMarkup)
	mux.HandleFunc("GET /cus/{uid}/markup", h.GetMarkup)
	mux.HandleFunc("POST /cus/{uid}/rebuild", h.Rebuild)
	mux.HandleFunc("GET /cus/{uid}/timecode", h.GetTimecode)
	mux.HandleFunc("GET /cus/{uid}/runs", h.GetRuns)
	mux.HandleFunc("GET /cus/{uid}/runs/{id}", h.GetRun)
	return recoverAndLog(cors(mux))
}
