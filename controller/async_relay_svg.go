package controller

import (
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strings"
)

var svgLocalURL = regexp.MustCompile(`(?i)url\(\s*['"]?#[a-zA-Z0-9_.:-]+['"]?\s*\)`)

// validateAsyncSVG 只接受静态矢量图；保留原始文件，不允许脚本、外部资源或嵌入网页。
func validateAsyncSVG(reader io.Reader) error {
	decoder := xml.NewDecoder(reader)
	allowed := " svg g defs symbol use title desc metadata path rect circle ellipse line polyline polygon text tspan textPath linearGradient radialGradient stop clipPath mask pattern filter feBlend feColorMatrix feComponentTransfer feComposite feConvolveMatrix feDiffuseLighting feDisplacementMap feDistantLight feDropShadow feFlood feFuncA feFuncB feFuncG feFuncR feGaussianBlur feMerge feMergeNode feMorphology feOffset fePointLight feSpecularLighting feSpotLight feTile feTurbulence style "
	depth, roots, elements := 0, 0, 0
	inStyle := false
	var style strings.Builder
	checkCSS := func(value string) bool {
		value = strings.ToLower(svgLocalURL.ReplaceAllString(value, ""))
		return !strings.ContainsAny(value, "\\@") && !strings.Contains(value, "url") && !strings.Contains(value, "expression") && !strings.Contains(value, "/*")
	}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			if roots != 1 || depth != 0 {
				return fmt.Errorf("SVG 文档结构不完整")
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("SVG 文档格式有误")
		}
		switch item := token.(type) {
		case xml.Directive:
			return fmt.Errorf("SVG 不允许文档类型或实体声明")
		case xml.ProcInst:
			if item.Target != "xml" || roots != 0 {
				return fmt.Errorf("SVG 不允许外部处理指令")
			}
		case xml.StartElement:
			elements++
			if elements > 100000 || depth > 128 {
				return fmt.Errorf("SVG 文档结构过于复杂")
			}
			if depth == 0 {
				roots++
				if item.Name.Local != "svg" || roots != 1 {
					return fmt.Errorf("SVG 根元素有误")
				}
			}
			depth++
			if (item.Name.Space != "" && item.Name.Space != "http://www.w3.org/2000/svg") || !strings.Contains(allowed, " "+item.Name.Local+" ") {
				return fmt.Errorf("SVG 包含不支持的动态或嵌入内容")
			}
			if item.Name.Local == "style" {
				inStyle = true
				style.Reset()
			}
			for _, attr := range item.Attr {
				name := strings.ToLower(attr.Name.Local)
				if attr.Name.Space == "xmlns" || name == "xmlns" {
					continue
				}
				if strings.HasPrefix(name, "on") || name == "base" || name == "src" {
					return fmt.Errorf("SVG 包含动态属性")
				}
				if name == "href" && !strings.HasPrefix(strings.TrimSpace(attr.Value), "#") {
					return fmt.Errorf("SVG 不允许外部资源")
				}
				if !checkCSS(attr.Value) {
					return fmt.Errorf("SVG 包含不安全的样式或资源引用")
				}
			}
		case xml.EndElement:
			if item.Name.Local == "style" {
				if !checkCSS(style.String()) {
					return fmt.Errorf("SVG 包含不安全的样式")
				}
				inStyle = false
			}
			depth--
		case xml.CharData:
			if inStyle {
				style.Write(item)
			}
			if depth == 0 && strings.TrimSpace(string(item)) != "" {
				return fmt.Errorf("SVG 根元素之外存在内容")
			}
		}
	}
}
