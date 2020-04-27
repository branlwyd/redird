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

	"github.com/BranLwyd/redird/assets"
	"github.com/BranLwyd/redird/handler"
	"github.com/golang/protobuf/proto"

	pb "github.com/BranLwyd/redird/redird_go_proto"
)

var (
	configFile = flag.String("config", "", "The redird configuration file to use.")

	categoryViewTmpl = template.Must(template.New("category-view").Parse(string(assets.MustAsset("assets/category.html"))))
)

func parseAndVerifyConfig(cfgBytes []byte) (*pb.Config, http.Handler, error) {
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

	mux := http.NewServeMux()
	mux.Handle("/style.css", handler.Must(handler.NewAsset("assets/style.css", "text/css; charset=utf-8")))

	if err := parseAndVerifyItem("/", cfg.Content, mux); err != nil {
		return nil, nil, fmt.Errorf("error parsing content: %v", err)
	}
	return cfg, mux, nil
}

func parseAndVerifyItem(pth string, item *pb.Item, mux *http.ServeMux) error {
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

	// Verify content-type specific fields & update mux appropriately.
	switch c := item.Content.(type) {
	case *pb.Item_Category:
		// Update mux.
		h, err := categoryHandler(pth, item)
		if err != nil {
			return fmt.Errorf("item %q invalid: %v", pth, err)
		}
		mux.Handle(pth, h)

		// Handle sub-items.
		// TODO: check for duplicate names
		for _, subItm := range c.Category.Item {
			subPth := path.Join(pth, subItm.Name)
			if pathNeedsTrailingSlash(subItm) {
				subPth = subPth + "/"
			}
			if err := parseAndVerifyItem(subPth, subItm, mux); err != nil {
				return err
			}
		}
		return nil

	case *pb.Item_Link:
		// Validate.
		if c.Link.Url == "" {
			return fmt.Errorf("item %q invalid: missing url")
		}

		// Update mux.
		mux.Handle(pth, handler.NewFiltered(pth, handler.NewRedirect(c.Link.Url)))
		return nil

	case nil:
		return fmt.Errorf("item %q invalid: no content", pth)

	default:
		return fmt.Errorf("item %q invalid: unexpected content-type %T", pth, c)
	}
}

func categoryHandler(pth string, itm *pb.Item) (http.Handler, error) {
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
				URL:         path.Join(pth, subItm.Name) + "/",
				Description: subItm.Description,
			})

		case *pb.Item_Link:
			links = append(links, Link{
				Name:        subItm.Name,
				URL:         path.Join(pth, subItm.Name),
				Description: subItm.Description,
			})

		default:
			return nil, fmt.Errorf("unknown content item type %T", c)
		}
	}

	var buf bytes.Buffer
	if err := categoryViewTmpl.Execute(&buf, struct {
		Title       string
		Description string
		Categories  []Category
		Links       []Link
	}{cat.Title, itm.Description, cats, links}); err != nil {
		return nil, fmt.Errorf("could not execute category-view template: %v", err)
	}
	return handler.NewFiltered(pth, handler.NewStatic(buf.Bytes(), "text/html; charset=utf-8")), nil
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
	cfg, h, err := parseAndVerifyConfig(cfgBytes)
	if err != nil {
		log.Fatalf("Couldn't parse/verify config file: %v", err)
	}
	serve(cfg.HostName, cfg.Email, cfg.CertDir, handler.NewSecureHeaderHandler(h))
}
