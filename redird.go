package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"path"
	"strings"
	"text/template"
	"time"

	"github.com/BranLwyd/redird/assets"
	"github.com/golang/protobuf/proto"

	pb "github.com/BranLwyd/redird/redird_go_proto"
)

// TODO: general code cleanup; split handlers out to a separate package
// TODO: all responses are static, so cache/pre-build responses at startup
// TODO: set cache headers so that clients can cache, too

var (
	configFile = flag.String("config", "", "The redird configuration file to use.")

	categoryViewTmpl = template.Must(template.New("category-view").Parse(string(assets.MustAsset("assets/category.html"))))
)

type loggingHandler struct {
	h       http.Handler
	logName string
}

func (lh loggingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Strip port from remote address, as the client port is not useful information.
	ra := r.RemoteAddr
	idx := strings.LastIndex(ra, ":")
	if idx != -1 {
		ra = ra[:idx]
	}
	log.Printf("[%s] %s requested %s", lh.logName, ra, r.RequestURI)
	lh.h.ServeHTTP(w, r)
}

func NewLoggingHandler(logName string, h http.Handler) http.Handler {
	return loggingHandler{
		h:       h,
		logName: logName,
	}
}

type secureHeaderHandler struct {
	h http.Handler
}

func (shh secureHeaderHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")
	w.Header().Add("X-Frame-Options", "DENY")
	w.Header().Add("X-XSS-Protection", "1; mode=block")
	w.Header().Add("X-Content-Type-Options", "nosniff")
	w.Header().Add("Content-Security-Policy", "default-src 'self'; style-src-elem 'self' https://fonts.googleapis.com; font-src https://fonts.gstatic.com")

	shh.h.ServeHTTP(w, r)
}

func NewSecureHeaderHandler(h http.Handler) http.Handler { return secureHeaderHandler{h} }

// staticHandler serves static content from memory.
type staticHandler struct {
	content     []byte
	contentType string
}

func newStatic(content []byte, contentType string) staticHandler {
	return staticHandler{
		content:     content,
		contentType: contentType,
	}
}

func mustNewAsset(name, contentType string) staticHandler {
	asset, ok := assets.Asset[name]
	if !ok {
		panic(fmt.Sprintf("No such asset %q", name))
	}
	return newStatic(asset, contentType)
}

func (sh staticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", sh.contentType)
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(sh.content))
}

type urlRedirector struct {
	itemByPath map[string]*pb.Item
}

func (u urlRedirector) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	itm, ok := u.itemByPath[r.URL.Path]
	if !ok {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}

	switch c := itm.Content.(type) {
	case *pb.Item_Category:
		serveCategory(w, r, itm)

	case *pb.Item_Link:
		http.Redirect(w, r, c.Link.Url, http.StatusFound)

	default:
		log.Printf("Path %q has unknown item type %T", r.URL.Path, c)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

func serveCategory(w http.ResponseWriter, r *http.Request, itm *pb.Item) {
	cat := itm.Content.(*pb.Item_Category).Category

	// Prepare category content to run template.
	type Category struct {
		Name        string
		URL         string
		Description string
	}
	type Link struct {
		Name        string
		URL         string
		Description string
	}
	var cats []Category
	var links []Link
	for _, subItm := range cat.Item {
		switch c := subItm.Content.(type) {
		case *pb.Item_Category:
			cats = append(cats, Category{
				Name:        subItm.Name,
				URL:         path.Join(r.URL.Path, subItm.Name) + "/",
				Description: subItm.Description,
			})

		case *pb.Item_Link:
			links = append(links, Link{
				Name:        subItm.Name,
				URL:         path.Join(r.URL.Path, subItm.Name),
				Description: subItm.Description,
			})

		default:
			log.Printf("Path %q has unknown content item type %T", r.URL.Path, c)
		}
	}

	var buf bytes.Buffer
	if err := categoryViewTmpl.Execute(&buf, struct {
		Title       string
		Description string
		Categories  []Category
		Links       []Link
	}{cat.Title, itm.Description, cats, links}); err != nil {
		log.Printf("Could not execute category-view template: %v", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(buf.Bytes()))
}

func parseAndVerifyConfig(cfgBytes []byte) (_ *pb.Config, itemByPath map[string]*pb.Item, _ error) {
	cfg := &pb.Config{}
	if err := proto.UnmarshalText(string(cfgBytes), cfg); err != nil {
		return nil, nil, fmt.Errorf("couldn't unmarshal: %v", err)
	}

	if cfg.HostName == "" {
		return nil, nil, errors.New("host_name is required")
	}
	if cfg.Email == "" {
		return nil, nil, errors.New("email is required")
	}
	if cfg.CertDir == "" {
		return nil, nil, errors.New("cert_dir is required")
	}

	itemByPath = map[string]*pb.Item{}
	if err := parseAndVerifyItem("/", cfg.Content, itemByPath); err != nil {
		return nil, nil, fmt.Errorf("error parsing content: %v", err)
	}
	return cfg, itemByPath, nil
}

func parseAndVerifyItem(pth string, item *pb.Item, itemByPath map[string]*pb.Item) error {
	isRoot := (pth == "/")

	// Verify name.
	if !isRoot && item.Name == "" {
		return fmt.Errorf("item under %q has no name", pth)
	}
	if isRoot && item.Name != "" {
		return errors.New("root item has a name")
	}
	// TODO: check that name is a valid URL path segment, rather than just doesn't contain a slash.
	if strings.Contains(item.Name, "/") {
		return fmt.Errorf("item %q invalid: item name contains a slash", pth)
	}

	// Verify content-type specific fields.
	switch c := item.Content.(type) {
	case *pb.Item_Category:
		// TODO: check for duplicate names
		for _, subItm := range c.Category.Item {
			subPth := path.Join(pth, subItm.Name)
			if pathNeedsTrailingSlash(subItm) {
				subPth = subPth + "/"
			}
			if err := parseAndVerifyItem(subPth, subItm, itemByPath); err != nil {
				return err
			}
		}

	case *pb.Item_Link:
		if c.Link.Url == "" {
			return fmt.Errorf("item %q invalid: missing url")
		}

	case nil:
		return fmt.Errorf("item %q invalid: no content", pth)

	default:
		return fmt.Errorf("item %q invalid: unexpected content-type %T", pth, c)
	}

	// Verification succeeded. Update itemByPath and return.
	itemByPath[pth] = item
	return nil
}

func pathNeedsTrailingSlash(item *pb.Item) bool {
	if _, ok := item.Content.(*pb.Item_Category); ok {
		return true
	}
	return false
}

func main() {
	// Parse & validate configuration.
	flag.Parse()
	if *configFile == "" {
		log.Fatalf("--config is required")
	}
	cfgBytes, err := ioutil.ReadFile(*configFile)
	if err != nil {
		log.Fatalf("Couldn't read config file %q: %v", *configFile, err)
	}
	cfg, itemByPath, err := parseAndVerifyConfig(cfgBytes)
	if err != nil {
		log.Fatalf("Couldn't parse/verify config file: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/style.css", mustNewAsset("assets/style.css", "text/css; charset=utf-8"))
	mux.Handle("/", urlRedirector{itemByPath})
	serve(cfg.HostName, cfg.Email, cfg.CertDir, NewSecureHeaderHandler(mux))
}
