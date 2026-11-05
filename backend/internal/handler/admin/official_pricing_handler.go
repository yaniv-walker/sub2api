package admin

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *ChannelHandler) officialPricingAvailable(c *gin.Context) bool {
	if h.officialPricing == nil {
		response.InternalError(c, "Official pricing service is unavailable")
		return false
	}
	return true
}

func (h *ChannelHandler) ListOfficialPricing(c *gin.Context) {
	if !h.officialPricingAvailable(c) {
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	if limit < 1 || limit > 200 {
		limit = 200
	}
	items, total, err := h.officialPricing.ListPage(c.Request.Context(), c.Query("platform"), c.Query("search"), limit)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"items": items, "total": total, "limit": limit})
}

func (h *ChannelHandler) GetOfficialPricing(c *gin.Context) {
	if !h.officialPricingAvailable(c) {
		return
	}
	entry, err := h.officialPricing.Get(c.Request.Context(), c.Query("platform"), c.Query("model"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, entry)
}

// SaveOfficialPricing replaces just this platform/model's reference override.
// A nil price inherits the automatic value; zero remains an explicit price.
func (h *ChannelHandler) SaveOfficialPricing(c *gin.Context) {
	if !h.officialPricingAvailable(c) {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 256*1024)
	var pricing service.OfficialPrice
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&pricing); err != nil {
		response.BadRequest(c, "Invalid official pricing document")
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		response.BadRequest(c, "Expected one official pricing document")
		return
	}
	entry, err := h.officialPricing.Save(c.Request.Context(), c.Query("platform"), c.Query("model"), pricing)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, entry)
}

func (h *ChannelHandler) DeleteOfficialPricing(c *gin.Context) {
	if !h.officialPricingAvailable(c) {
		return
	}
	if err := h.officialPricing.Delete(c.Request.Context(), c.Query("platform"), c.Query("model")); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "Official price override cleared"})
}

func officialDefaultPricingResponse(entry *service.OfficialPricingEntry) gin.H {
	p := entry.Pricing
	return gin.H{"found": true, "source": entry.Source, "billing_mode": p.BillingMode, "input_price": p.InputPrice, "output_price": p.OutputPrice, "cache_write_price": p.CacheWritePrice, "cache_write_1h_price": p.CacheWrite1hPrice, "cache_read_price": p.CacheReadPrice, "image_input_price": p.ImageInputPrice, "image_output_price": p.ImageOutputPrice, "per_request_price": p.PerRequestPrice, "intervals": p.Intervals}
}
