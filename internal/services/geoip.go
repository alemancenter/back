package services

import (
	"net"
	"sync"

	"github.com/imanjo/fiber-api/internal/config"
	"github.com/imanjo/fiber-api/pkg/logger"
	"github.com/oschwald/geoip2-golang"
	"go.uber.org/zap"
)

var (
	geoipReader   *geoip2.Reader
	geoipInitOnce sync.Once
)

func geoIPReader() *geoip2.Reader {
	geoipInitOnce.Do(func() {
		path := config.Get().GeoIP.DBPath
		if path == "" {
			logger.Warn("GEOIP_DB_PATH not set — visitor country/city will stay empty until a GeoLite2-City database is configured. See .env.example.")
			return
		}
		reader, err := geoip2.Open(path)
		if err != nil {
			logger.Warn("failed to open GeoIP database — visitor country/city will stay empty",
				zap.String("path", path), zap.Error(err))
			return
		}
		geoipReader = reader
		logger.Info("GeoIP database loaded", zap.String("path", path))
	})
	return geoipReader
}

// GeoIPResult is a best-effort IP → location lookup. Country/City are empty strings (never an
// error) when the database isn't configured, the address is private/reserved, or it simply
// isn't found — geolocation is an enrichment for visitor analytics, not something that should
// ever block or fail visitor tracking itself.
type GeoIPResult struct {
	Country   string
	City      string
	Latitude  float64
	Longitude float64
}

// LookupGeoIP resolves an IP address using the local MaxMind GeoLite2-City database configured
// via GEOIP_DB_PATH (see .env.example). Safe for concurrent use.
func LookupGeoIP(ipAddress string) GeoIPResult {
	reader := geoIPReader()
	if reader == nil {
		return GeoIPResult{}
	}
	ip := net.ParseIP(ipAddress)
	if ip == nil || ip.IsPrivate() || ip.IsLoopback() {
		return GeoIPResult{}
	}
	record, err := reader.City(ip)
	if err != nil {
		return GeoIPResult{}
	}
	return GeoIPResult{
		Country:   record.Country.Names["en"],
		City:      record.City.Names["en"],
		Latitude:  record.Location.Latitude,
		Longitude: record.Location.Longitude,
	}
}

// CloseGeoIP releases the underlying database file handle. Call during graceful shutdown.
func CloseGeoIP() {
	if geoipReader != nil {
		_ = geoipReader.Close()
	}
}
