package main

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"zgo.at/goatcounter/v2"
	"zgo.at/goatcounter/v2/handlers"
	"zgo.at/goatcounter/v2/pkg/log"
	"zgo.at/zhttp"
	"zgo.at/zli"
	"zgo.at/zstd/znet"
	"zgo.at/zvalidate"
)

const usageSaas = `
This runs goatcounter.com

Running your own SaaS is currently undocumented, non-trivial, and has certain
assumptions that will not be true in your case. You do not want to run this; for
now it can only run run goatcounter.com

If you do want to run a SaaS, you're almost certainly better off writing your
own front-end to interface with GoatCounter (this is probably how
goatcounter.com should work as well, but it's quite some effort with low ROI to
change that now).

This command is undocumented on purpose. Get in touch if you think you need this
(but you probably don't) and we'll see what can be done to fix you up.
`

func cmdSaas(f zli.Flags, ready chan<- struct{}, stop chan struct{}) error {
	v := zvalidate.New()

	var (
		domain = f.String("goatcounter.localhost:8081,static.goatcounter.localhost:8081", "domain").Pointer()
	)
	dbConnect, dbConn, dev, automigrate, listen, flagTLS, from, websocket, apiMax, ratelimits, geodb, err := flagsServe(f, &v)
	if err != nil {
		return err
	}

	return func(domain string) error {
		if flagTLS == "" {
			flagTLS = map[bool]string{true: "http", false: "acme"}[dev]
		}

		domain, domainStatic, domainCount, urlStatic := flagDomain(domain, &v)
		from = flagFrom(from, domain, &v)
		if !dev && domain != "goatcounter.com" {
			v.Append("saas", "can only run on goatcounter.com")
		}

		if v.HasErrors() {
			return v
		}

		db, ctx, tlsc, acmeh, listenTLS, err := setupServe(dbConnect, dbConn, dev, flagTLS, automigrate, geodb)
		if err != nil {
			return err
		}

		c := goatcounter.Config(ctx)
		c.GoatcounterCom = true
		c.Dev = dev
		c.Domain = domain
		c.DomainStatic = domainStatic
		c.DomainCount = domainCount
		c.URLStatic = urlStatic
		c.EmailFrom = from
		c.Websocket = websocket

		// Set up HTTP handler and servers.
		d := znet.RemovePort(domain)
		hosts := map[string]http.Handler{
			d:          zhttp.RedirectHost("https://www." + domain),
			"www." + d: handlers.NewWebsite(db, dev),
			"*":        handlers.NewBackend(db, acmeh, dev, c.GoatcounterCom, websocket, c.DomainStatic, c.BasePath, 15, apiMax, ratelimits),
		}
		if dev {
			hosts[znet.RemovePort(domainStatic)] = handlers.NewStatic(chi.NewRouter(), dev, true, c.BasePath)
		}

		return doServe(ctx, db, listen, listenTLS, tlsc, hosts, stop, func() {
			log.Module("startup").Info(ctx, "GoatCounter ready", startupAttr(geodb, listen, dev,
				"domain", domain,
			)...)
			ready <- struct{}{}
		})
	}(*domain)
}

func flagDomain(domain string, v *zvalidate.Validator) (string, string, string, string) {
	l := strings.Split(domain, ",")

	var (
		rDomain      string
		domainStatic string
		domainCount  string
		urlStatic    string
	)
	switch len(l) {
	default:
		v.Append("-domain", "too many domains")
	case 0:
		v.Append("-domain", "cannot be blank")
	case 1:
		v.Append("-domain", "must have static domain")
	case 2, 3:
		for i, d := range l {
			d = strings.TrimSpace(d)
			if p := strings.Index(d, ":"); p > -1 {
				v.Domain("-domain", d[:p])
			} else {
				v.Domain("-domain", d)
			}

			switch i {
			case 0:
				rDomain = d
			case 1:
				domainStatic = d
				domainCount = d
				urlStatic = "//" + d
			case 2:
				domainCount = d
			}
		}
	}
	return rDomain, domainStatic, domainCount, urlStatic
}
