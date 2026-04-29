package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// PaginatedResponse matches the frontend PaginatedResponse<T> interface
type PaginatedResponse struct {
	Items      interface{} `json:"items"`
	Total      int         `json:"total"`
	Page       int         `json:"page"`
	PageSize   int         `json:"page_size"`
	TotalPages int         `json:"total_pages"`
}

// Success sends a 200 response with { "data": T } wrapper
func Success(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, gin.H{"data": data})
}

// SuccessCreated sends a 201 response with { "data": T } wrapper
func SuccessCreated(c *gin.Context, data interface{}) {
	c.JSON(http.StatusCreated, gin.H{"data": data})
}

// SuccessList sends a 200 response with paginated data wrapper
// Format: { "data": { "items": [], "total": N, "page": P, "page_size": S, "total_pages": TP } }
func SuccessList(c *gin.Context, items interface{}, total, page, pageSize int) {
	totalPages := 0
	if pageSize > 0 {
		totalPages = (total + pageSize - 1) / pageSize
	}

	c.JSON(http.StatusOK, gin.H{
		"data": PaginatedResponse{
			Items:      items,
			Total:      total,
			Page:       page,
			PageSize:   pageSize,
			TotalPages: totalPages,
		},
	})
}

// Error sends an error response with { "error": message }
func Error(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"error": message})
}

// ErrorBadRequest sends a 400 error
func ErrorBadRequest(c *gin.Context, message string) {
	Error(c, http.StatusBadRequest, message)
}

// ErrorUnauthorized sends a 401 error
func ErrorUnauthorized(c *gin.Context, message string) {
	Error(c, http.StatusUnauthorized, message)
}

// ErrorNotFound sends a 404 error
func ErrorNotFound(c *gin.Context, message string) {
	Error(c, http.StatusNotFound, message)
}

// ErrorConflict sends a 409 error
func ErrorConflict(c *gin.Context, message string) {
	Error(c, http.StatusConflict, message)
}

// ErrorInternal sends a 500 error
func ErrorInternal(c *gin.Context, message string) {
	Error(c, http.StatusInternalServerError, message)
}
