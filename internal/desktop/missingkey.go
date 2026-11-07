package desktop

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"time"
)

// missingKeyPage explains, without the panel, that the existing database cannot be opened because its secret key
// is not available (m6a-plan §2.5). It is static: no data, no key, no path.
const missingKeyPage = `<!doctype html>
<html lang="it"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Porto: chiave segreta mancante</title></head>
<body style="font-family: system-ui, sans-serif; max-width: 40rem; margin: 3rem auto; padding: 0 1rem; line-height: 1.5">
<h1>Chiave segreta mancante</h1>
<p>I dati di Porto su questo computer sono cifrati con una chiave segreta che non si trova più nel portachiavi del
sistema né nella cartella dei dati. Porto non ne crea una nuova, perché i dati cifrati andrebbero persi.</p>
<p>Per riaprire i dati:</p>
<ol>
<li>avvia Porto con la variabile d'ambiente <code>PORTO_SECRET_KEY</code> impostata alla chiave conservata, oppure</li>
<li>sposta altrove la cartella dei dati e ripristina un backup insieme alla sua chiave.</li>
</ol>
<hr>
<h2 lang="en">Secret key missing</h2>
<p lang="en">Porto's data on this computer is encrypted with a secret key that is no longer in the system keychain
or in the data folder. Porto does not create a new one, because the encrypted data would be lost. Start Porto with
<code>PORTO_SECRET_KEY</code> set to the saved key, or move the data folder away and restore a backup with its key.</p>
</body></html>`

// showMissingKey serves missingKeyPage on loopback and shows it until the UI closes, then returns cause.
func (l *Launcher) showMissingKey(ctx context.Context, cause error) error {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return cause
	}
	host := "127.0.0.1:" + strconv.Itoa(listenerPort(ln))
	srv := &http.Server{
		ReadHeaderTimeout: 5 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Host != host {
				http.Error(w, "misdirected", http.StatusMisdirectedRequest)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
			_, _ = w.Write([]byte(missingKeyPage))
		}),
	}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()
	url := "http://" + host + "/"
	if l.o.OnReady != nil {
		l.o.OnReady(url)
	}
	_ = l.ui.Run(ctx, url)
	if errors.Is(cause, ErrSecretInvalid) {
		return cause
	}
	return ErrSecretMissing
}
