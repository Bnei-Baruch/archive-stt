// Package api: the HTTP API (net/http).
//
//	POST /cus/{uid}/markup?language=&force=false   a first run -> 202 + the run   (X-API-Key if keys are set, else from this host only)
//	POST /cus/{uid}/rebuild?run=                   a new revision auto from a run's kept words and diarization
//	                                               and the transcript now, no ai-trns -> 201 (200 if unchanged)
//	GET  /cus/{uid}/markup?rev=                    the markup document, public; 404 until a run is ready
//	GET  /cus/{uid}/timecode?lang=                 the time of every word of the transcript: paragraphs[p][w] --
//	                                               the w-th word (by whitespace) of the p-th <p> of its doc2html;
//	                                               public; 404 without markup on a transcript
//	GET  /cus?lang=                                the content units whose current revision has no transcript:
//	                                               [{uid, language}]; public
//	GET  /cus/{uid}/content?lang=                  the heard words as plain text, paragraphs apart by an empty
//	                                               line (for a search engine, where there is no transcript); public
//	GET  /cus/{uid}/locate?q=&lang=                fragments of the text (a search engine's highlights) -- the
//	                                               transcript's, else the heard words -- by their first and last
//	                                               words and length: q=<first>|<last>|<n>, the last two optional,
//	                                               up to 20; where (p, w: in the transcript only), when (start,
//	                                               end), the chapter; public
//	GET  /cus/{uid}/text?at= | ?from=&to=, &lang=  the words (the transcript's, else the heard ones), paragraphs
//	                                               and chapters at a moment or in a span (up to 15 min); public
//	GET  /cus/{uid}/find?q=&lang=&limit=10         where the phrase is said: [{start, end, score, context,
//	                                               paragraph, chapter}], the best first; public
//	GET  /cus/{uid}/runs                           the lesson's runs, newest first, public
//	GET  /cus/{uid}/runs/latest | /runs/{id}       one run, public
//	GET  /health
package api

import "net/http"

// Handler is the API with its middleware
func Handler(h *Handlers) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", h.Health)
	mux.HandleFunc("GET /cus", h.GetContentUnits)
	mux.HandleFunc("GET /cus/{uid}/content", h.GetContent)
	mux.HandleFunc("POST /cus/{uid}/markup", h.StartMarkup)
	mux.HandleFunc("GET /cus/{uid}/markup", h.GetMarkup)
	mux.HandleFunc("POST /cus/{uid}/rebuild", h.Rebuild)
	mux.HandleFunc("GET /cus/{uid}/timecode", h.GetTimecode)
	mux.HandleFunc("GET /cus/{uid}/locate", h.GetLocate)
	mux.HandleFunc("GET /cus/{uid}/text", h.GetText)
	mux.HandleFunc("GET /cus/{uid}/find", h.GetFind)
	mux.HandleFunc("GET /cus/{uid}/runs", h.GetRuns)
	mux.HandleFunc("GET /cus/{uid}/runs/{id}", h.GetRun)
	return recoverAndLog(cors(mux))
}
