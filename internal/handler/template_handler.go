package handler

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/bioinfo/schema-platform/internal/config"
	"github.com/bioinfo/schema-platform/internal/model"
	"github.com/gin-gonic/gin"
)

var templateDir string

func initTemplates() {
	templateDir = config.GetEnv("TEMPLATE_DIR", "/home/ubuntu/schema-germline")
}

// ListTemplates godoc
// @Summary List available WDL templates
// @Description Get a list of available WDL workflow templates
// @Tags templates
// @Produce json
// @Success 200 {array} model.Template
// @Router /api/v1/templates [get]
func ListTemplates(c *gin.Context) {
	initTemplates()

	var templates []model.Template

	// Read WDL files from template directory
	files, err := os.ReadDir(templateDir)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read template directory"})
		return
	}

	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".wdl") {
			name := strings.TrimSuffix(file.Name(), ".wdl")
			templates = append(templates, model.Template{
				Name: name,
				Path: filepath.Join(templateDir, file.Name()),
			})
		}
	}

	c.JSON(http.StatusOK, templates)
}

// GetTemplate godoc
// @Summary Get template details
// @Description Get detailed information about a specific WDL template
// @Tags templates
// @Produce json
// @Param name path string true "Template name"
// @Success 200 {object} model.Template
// @Failure 404 {object} map[string]string
// @Router /api/v1/templates/{name} [get]
func GetTemplate(c *gin.Context) {
	initTemplates()

	name := c.Param("name")
	wdlPath := filepath.Join(templateDir, name+".wdl")

	// Check if file exists
	if _, err := os.Stat(wdlPath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "template not found"})
		return
	}

	// Read WDL file content
	content, err := os.ReadFile(wdlPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read template"})
		return
	}

	c.JSON(http.StatusOK, model.Template{
		Name: name,
		Path: wdlPath,
		Description: "WDL workflow template",
		InputFields: parseWDLInputs(string(content)),
	})
}

// parseWDLInputs extracts input field names from WDL content
func parseWDLInputs(content string) []string {
	var inputs []string
	lines := strings.Split(content, "\n")

	inWorkflow := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "workflow ") {
			inWorkflow = true
			continue
		}

		if inWorkflow && strings.HasPrefix(trimmed, "}") {
			break
		}

		if inWorkflow && strings.HasPrefix(trimmed, "input {") {
			continue
		}

		if inWorkflow && (strings.HasPrefix(trimmed, "String ") ||
			strings.HasPrefix(trimmed, "Int ") ||
			strings.HasPrefix(trimmed, "Float ") ||
			strings.HasPrefix(trimmed, "File ") ||
			strings.HasPrefix(trimmed, "Array ") ||
			strings.HasPrefix(trimmed, "Boolean ")) {
			// Extract input name
			parts := strings.Fields(trimmed)
			if len(parts) >= 2 {
				name := strings.TrimSuffix(parts[1], ",")
				inputs = append(inputs, name)
			}
		}
	}

	return inputs
}