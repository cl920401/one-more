package p1

// Ref identifies an exact material version.
type Ref struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

func (r Ref) String() string { return r.ID + "@" + r.Version }

type Paragraph struct {
	ID      string `json:"id"`
	Date    string `json:"date"`
	Speaker string `json:"speaker"`
	Text    string `json:"text"`
}

type Material struct {
	Ref        Ref         `json:"ref"`
	Paragraphs []Paragraph `json:"paragraphs"`
}

type Citation struct {
	Ref       Ref    `json:"ref"`
	Paragraph string `json:"paragraph"`
}

type Entry struct {
	ID          string     `json:"id"`
	Category    string     `json:"category"`
	Scope       string     `json:"scope"`
	Topic       string     `json:"topic"`
	Description string     `json:"description"`
	Question    string     `json:"question"`
	Citations   []Citation `json:"citations"`
}

// syntheticMaterials contains only the built-in fictional example fragments.
// Each call returns fresh slices. Attachment B is deliberately absent.
func syntheticMaterials() []Material {
	return []Material{
		{
			Ref: Ref{ID: "S-A", Version: "v1"},
			Paragraphs: []Paragraph{
				{ID: "P01", Date: "2026-11-02", Speaker: "双方约定", Text: "合成业务代表代表客户确认本试点需求与报价；合成期限代表只可确认甲产品试点的录音保存期限，无报价确认权。"},
				{ID: "P02", Date: "2026-11-02", Speaker: "合成业务代表", Text: "甲产品试点录音保存 90 天，乙产品维护期录音保存 30 天；报价尚未确认。"},
			},
		},
		{
			Ref: Ref{ID: "S-B", Version: "v1"},
			Paragraphs: []Paragraph{
				{ID: "P01", Date: "2026-11-04", Speaker: "合成期限代表", Text: "甲产品试点录音至少保存 180 天。"},
				{ID: "P02", Date: "2026-11-04", Speaker: "合成期限代表", Text: "我认为报价可以定为一万元。"},
				{ID: "P03", Date: "2026-11-04", Speaker: "合成业务代表", Text: "具体验收标准见附件 B。"},
			},
		},
		{
			Ref: Ref{ID: "S-C", Version: "v1"},
			Paragraphs: []Paragraph{
				{ID: "P01", Date: "2026-11-06", Speaker: "合成业务代表", Text: "甲产品试点的保存期限改为 180 天，替代 11 月 2 日的 90 天；乙产品维护期仍为 30 天。"},
			},
		},
	}
}

// buildEntries is a deterministic illustration for the exact built-in fixtures,
// not a general text classifier. Unknown versions, changed metadata or text, and
// unknown paragraphs do not supply evidence. Inconsistent duplicate paragraph
// identities are suppressed so citations cannot ambiguously identify content.
// Admission/authorization of the supplied materials belongs to the caller.
func buildEntries(materials []Material) []Entry {
	// Validate only supplied evidence against body-free compiled fingerprints.
	// Never reload fixture text by identity while classifying a payload.
	present := make(map[Citation]bool)
	invalid := make(map[Citation]bool)
	for _, material := range materials {
		for _, paragraph := range material.Paragraphs {
			expected := expectedFixtureParagraphFingerprint(material.Ref, paragraph.ID)
			if expected == "" {
				continue
			}
			citation := Citation{Ref: material.Ref, Paragraph: paragraph.ID}
			if suppliedParagraphFingerprint(material.Ref, paragraph) != expected {
				invalid[citation] = true
				delete(present, citation)
			} else if !invalid[citation] {
				present[citation] = true
			}
		}
	}
	a := Ref{ID: "S-A", Version: "v1"}
	b := Ref{ID: "S-B", Version: "v1"}
	c := Ref{ID: "S-C", Version: "v1"}
	authority := Citation{Ref: a, Paragraph: "P01"}
	oldTerms := Citation{Ref: a, Paragraph: "P02"}
	newTerm := Citation{Ref: b, Paragraph: "P01"}
	price := Citation{Ref: b, Paragraph: "P02"}
	acceptance := Citation{Ref: b, Paragraph: "P03"}
	replacement := Citation{Ref: c, Paragraph: "P01"}
	citations := func(candidates ...Citation) []Citation {
		result := make([]Citation, 0, len(candidates))
		for _, candidate := range candidates {
			if present[candidate] {
				result = append(result, candidate)
			}
		}
		return result
	}
	entries := make([]Entry, 0, 4)
	if present[oldTerms] || present[newTerm] || present[replacement] {
		entry := Entry{
			ID: "alpha-retention", Category: "待补问", Scope: "甲产品试点", Topic: "录音保存期限",
			Citations: citations(authority, oldTerms, newTerm, replacement),
		}
		switch {
		case present[authority] && present[oldTerms] && present[replacement]:
			entry.Category = "明确"
			entry.Description = "甲产品试点录音保存 180 天；同范围的明示替代、原 90 天依据及事项授权均有对应段落。"
		case present[oldTerms] && (present[newTerm] || present[replacement]):
			entry.Category = "冲突"
			entry.Description = "甲产品试点录音保存期限存在 90 天与 180 天相关的不兼容说法，现有证据尚未同时满足有效替代的全部条件。"
			if present[newTerm] {
				entry.Description = "甲产品试点录音保存 90 天与至少保存 180 天存在一项未解决分歧；较新说法不能仅凭日期覆盖旧值。"
			}
			entry.Question = "请补充同范围、明示替代且有对应事项授权的保存期限确认依据。"
		case present[replacement]:
			entry.Description = "材料提出甲产品试点改为保存 180 天并替代原 90 天，但核对该变更所需的原值或事项授权依据不完整。"
			entry.Question = "请补充原 90 天的材料及本事项授权依据，以核对替代是否有效。"
		case present[oldTerms] && present[authority]:
			entry.Category = "明确"
			entry.Description = "甲产品试点录音保存 90 天。"
		case present[newTerm] && present[authority]:
			entry.Category = "明确"
			entry.Description = "甲产品试点录音至少保存 180 天；当前传入材料中没有另一项已识别的期限说法。"
		case present[oldTerms]:
			entry.Description = "材料载明甲产品试点录音保存 90 天，但缺少对应事项授权依据。"
			entry.Question = "请补充甲产品试点保存期限的事项授权依据。"
		default:
			entry.Description = "合成期限代表提出甲产品试点录音至少保存 180 天，但缺少对应事项授权依据。"
			entry.Question = "请补充合成期限代表对甲产品试点保存期限的事项授权依据。"
		}
		entries = append(entries, entry)
	}
	if present[oldTerms] || present[replacement] {
		entry := Entry{
			ID: "beta-retention", Category: "待补问", Scope: "乙产品维护期", Topic: "录音保存期限",
			Description: "材料载明乙产品维护期录音保存 30 天，但缺少对应事项授权依据。",
			Question:    "请补充乙产品维护期保存期限的事项授权依据。",
			Citations:   citations(authority, oldTerms, replacement),
		}
		if present[authority] {
			entry.Category = "明确"
			entry.Description = "乙产品维护期录音保存 30 天；与甲产品试点分别按适用范围整理。"
			entry.Question = ""
		}
		entries = append(entries, entry)
	}
	if present[oldTerms] || present[price] {
		entry := Entry{
			ID: "quote", Category: "待补问", Scope: "本试点", Topic: "报价",
			Description: "材料载明报价尚未确认。",
			Question:    "请具有本事项报价确认权的代表明确报价及适用范围。",
			Citations:   citations(authority, oldTerms, price),
		}
		if present[price] {
			entry.Description = "合成期限代表提出一万元报价建议，现有材料未提供有报价确认权代表对此报价的确认；期限权限不能扩张到报价。"
		}
		entries = append(entries, entry)
	}
	if present[acceptance] {
		entries = append(entries, Entry{
			ID: "acceptance", Category: "待补问", Scope: "本试点", Topic: "验收标准",
			Description: "材料仅说明具体验收标准见附件 B，内置材料不包含该附件内容。",
			Question:    "请提供获准使用的具体验收标准材料。",
			Citations:   citations(acceptance),
		})
	}
	return entries
}
