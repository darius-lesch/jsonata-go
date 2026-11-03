// Copyright 2018 Blues Inc.  All rights reserved.
// Use of this source code is governed by licenses granted by the
// copyright holder including that found in the LICENSE file.

package jlib

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/darius-lesch/jsonata-go/v2/jtypes"
)

const defaultFormatTimeLayout = "[Y0001]-[M01]-[D01]T[H01]:[m01]:[s01].[f001][Z01:01t]"

type textCase string

const (
	tcaseLower textCase = "lower"
	tcaseUpper textCase = "upper"
	tcaseTitle textCase = "title"
)

type intFormatType string

const (
	formatDecimal  intFormatType = "decimal"
	formatLetters  intFormatType = "letters"
	formatRomanNum intFormatType = "roman"
	formatWordsNum intFormatType = "words"
	formatSequence intFormatType = "sequence"
)

type groupingSeparator struct {
	position  int
	character string
}

type integerFormatSpec struct {
	primary            intFormatType
	tcase              textCase
	ordinal            bool
	zeroCode           rune
	mandatoryDigits    int
	optionalDigits     int
	regular            bool
	groupingSeparators []groupingSeparator
	token              string
	parseWidth         int
}

type widthSpec struct {
	min    int
	hasMin bool
	max    int
	hasMax bool
}

type dateTimePart struct {
	partType      string // "literal" or "marker"
	value         string // for literal
	component     byte
	width         widthSpec
	hasWidth      bool
	presentation1 string
	presentation2 byte
	ordinal       bool
	names         textCase
	integerFormat *integerFormatSpec
	n             int
}

type dateTimeFormatSpec struct {
	parts []*dateTimePart
}

var defaultPresentationModifiers = map[byte]string{
	'Y': "1", 'M': "1", 'D': "1", 'd': "1", 'F': "n", 'W': "1", 'w': "1", 'X': "1", 'x': "1", 'H': "1", 'h': "1",
	'P': "n", 'm': "01", 's': "01", 'f': "1", 'Z': "01:01", 'z': "01:01", 'C': "n", 'E': "n",
}

var decimalGroups = []rune{
	0x30, 0x0660, 0x06F0, 0x07C0, 0x0966, 0x09E6, 0x0A66, 0x0AE6, 0x0B66, 0x0BE6,
	0x0C66, 0x0CE6, 0x0D66, 0x0DE6, 0x0E50, 0x0ED0, 0x0F20, 0x1040, 0x1090, 0x17E0,
	0x1810, 0x1946, 0x19D0, 0x1A80, 0x1A90, 0x1B50, 0x1BB0, 0x1C40, 0x1C50, 0xA620,
	0xA8D0, 0xA900, 0xA9D0, 0xA9F0, 0xAA50, 0xABF0, 0xFF10,
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

func analyseIntegerPicture(picture string) (*integerFormatSpec, error) {
	format := &integerFormatSpec{
		primary: formatDecimal,
		tcase:   tcaseLower,
	}

	var primaryFormat, formatModifier string
	semicolon := strings.LastIndex(picture, ";")
	if semicolon == -1 {
		primaryFormat = picture
	} else {
		primaryFormat = picture[:semicolon]
		formatModifier = picture[semicolon+1:]
		if len(formatModifier) > 0 && formatModifier[0] == 'o' {
			format.ordinal = true
		}
	}

	switch primaryFormat {
	case "A":
		format.tcase = tcaseUpper
		format.primary = formatLetters
	case "a":
		format.primary = formatLetters
	case "I":
		format.tcase = tcaseUpper
		format.primary = formatRomanNum
	case "i":
		format.primary = formatRomanNum
	case "W":
		format.tcase = tcaseUpper
		format.primary = formatWordsNum
	case "Ww":
		format.tcase = tcaseTitle
		format.primary = formatWordsNum
	case "w":
		format.primary = formatWordsNum
	default:
		var zeroCode rune
		hasZeroCode := false
		mandatoryDigits := 0
		optionalDigits := 0
		var groupingSeparators []groupingSeparator
		separatorPosition := 0

		runes := []rune(primaryFormat)
		for i := len(runes) - 1; i >= 0; i-- {
			codePoint := runes[i]
			digit := false
			for _, group := range decimalGroups {
				if codePoint >= group && codePoint <= group+9 {
					digit = true
					mandatoryDigits++
					separatorPosition++
					if !hasZeroCode {
						zeroCode = group
						hasZeroCode = true
					} else if group != zeroCode {
						return nil, fmt.Errorf("D3131: The picture string must not contain different zero digits")
					}
					break
				}
			}
			if !digit {
				if codePoint == '#' {
					separatorPosition++
					optionalDigits++
				} else {
					groupingSeparators = append(groupingSeparators, groupingSeparator{
						position:  separatorPosition,
						character: string(codePoint),
					})
				}
			}
		}

		if mandatoryDigits > 0 {
			format.primary = formatDecimal
			format.zeroCode = zeroCode
			format.mandatoryDigits = mandatoryDigits
			format.optionalDigits = optionalDigits

			regular := regularRepeat(groupingSeparators)
			if regular > 0 {
				format.regular = true
				format.groupingSeparators = []groupingSeparator{{
					position:  regular,
					character: groupingSeparators[0].character,
				}}
			} else {
				format.regular = false
				format.groupingSeparators = groupingSeparators
			}
		} else {
			format.primary = formatSequence
			format.token = primaryFormat
		}
	}

	return format, nil
}

func regularRepeat(separators []groupingSeparator) int {
	if len(separators) == 0 {
		return 0
	}
	sepChar := separators[0].character
	for i := 1; i < len(separators); i++ {
		if separators[i].character != sepChar {
			return 0
		}
	}
	factor := separators[0].position
	for i := 1; i < len(separators); i++ {
		factor = gcd(factor, separators[i].position)
	}
	if factor <= 0 {
		return 0
	}
	for index := 1; index <= len(separators); index++ {
		found := false
		for _, sep := range separators {
			if sep.position == index*factor {
				found = true
				break
			}
		}
		if !found {
			return 0
		}
	}
	return factor
}

func formatAnalyzedInteger(value int64, format *integerFormatSpec) (string, error) {
	negative := value < 0
	if negative {
		value = -value
	}

	var formattedInteger string
	switch format.primary {
	case formatLetters:
		formattedInteger = formatSpreadsheet(value, format.tcase == tcaseUpper)
	case formatRomanNum:
		formattedInteger = formatRoman(value, format.tcase == tcaseUpper)
	case formatWordsNum:
		formattedInteger = formatWords(value, format.ordinal)
		switch format.tcase {
		case tcaseUpper:
			formattedInteger = strings.ToUpper(formattedInteger)
		case tcaseTitle:
			formattedInteger = titleCaseWords(formattedInteger)
		}
	case formatDecimal:
		formattedInteger = strconv.FormatInt(value, 10)
		padLength := format.mandatoryDigits - len(formattedInteger)
		if padLength > 0 {
			formattedInteger = strings.Repeat("0", padLength) + formattedInteger
		}
		if format.zeroCode != '0' {
			var b strings.Builder
			for _, r := range formattedInteger {
				b.WriteRune(r + format.zeroCode - '0')
			}
			formattedInteger = b.String()
		}
		runes := []rune(formattedInteger)
		if format.regular && len(format.groupingSeparators) > 0 {
			posStep := format.groupingSeparators[0].position
			sepChar := []rune(format.groupingSeparators[0].character)
			n := (len(runes) - 1) / posStep
			for ii := n; ii > 0; ii-- {
				pos := len(runes) - ii*posStep
				runes = append(runes[:pos], append(sepChar, runes[pos:]...)...)
			}
			formattedInteger = string(runes)
		} else if len(format.groupingSeparators) > 0 {
			for i := len(format.groupingSeparators) - 1; i >= 0; i-- {
				sep := format.groupingSeparators[i]
				pos := len(runes) - sep.position
				if pos > 0 && pos < len(runes) {
					sepChar := []rune(sep.character)
					runes = append(runes[:pos], append(sepChar, runes[pos:]...)...)
				}
			}
			formattedInteger = string(runes)
		}
		if format.ordinal {
			formattedInteger += getOrdinalSuffix(value)
		}
	case formatSequence:
		return "", fmt.Errorf("D3130: The picture string %q is invalid", format.token)
	}

	if negative {
		formattedInteger = "-" + formattedInteger
	}
	return formattedInteger, nil
}

var reMarkerWhitespace = regexp.MustCompile(`\s+`)

func analyseDateTimePicture(picture string) (*dateTimeFormatSpec, error) {
	var spec []*dateTimePart
	addLiteral := func(start, end int) {
		if end > start {
			literal := picture[start:end]
			literal = strings.ReplaceAll(literal, "]]", "]")
			spec = append(spec, &dateTimePart{
				partType: "literal",
				value:    literal,
			})
		}
	}

	start, pos := 0, 0
	for pos < len(picture) {
		if picture[pos] == '[' {
			if pos+1 < len(picture) && picture[pos+1] == '[' {
				addLiteral(start, pos)
				spec = append(spec, &dateTimePart{
					partType: "literal",
					value:    "[",
				})
				pos += 2
				start = pos
				continue
			}
			addLiteral(start, pos)
			start = pos
			closeIdx := strings.IndexByte(picture[start:], ']')
			if closeIdx == -1 {
				return nil, fmt.Errorf("D3135: No closing bracket in picture string")
			}
			pos = start + closeIdx
			marker := picture[start+1 : pos]
			marker = reMarkerWhitespace.ReplaceAllString(marker, "")
			if len(marker) == 0 {
				return nil, fmt.Errorf("D3132: Empty component specifier")
			}
			def := &dateTimePart{
				partType:  "marker",
				component: marker[0],
				n:         -1,
			}
			comma := strings.LastIndexByte(marker, ',')
			var presMod string
			if comma != -1 {
				widthMod := marker[comma+1:]
				dash := strings.IndexByte(widthMod, '-')
				var minStr, maxStr string
				if dash == -1 {
					minStr = widthMod
				} else {
					minStr = widthMod[:dash]
					maxStr = widthMod[dash+1:]
				}
				var wSpec widthSpec
				if minStr != "" && minStr != "*" {
					v, err := strconv.Atoi(minStr)
					if err != nil || v < 0 {
						return nil, fmt.Errorf("invalid width modifier %q", minStr)
					}
					wSpec.min = v
					wSpec.hasMin = true
				}
				if maxStr != "" && maxStr != "*" {
					v, err := strconv.Atoi(maxStr)
					if err != nil || v < 0 {
						return nil, fmt.Errorf("invalid width modifier %q", maxStr)
					}
					wSpec.max = v
					wSpec.hasMax = true
				}
				def.width = wSpec
				def.hasWidth = true
				presMod = marker[1:comma]
			} else {
				presMod = marker[1:]
			}

			if _, ok := defaultPresentationModifiers[def.component]; !ok {
				return nil, fmt.Errorf("D3132: Unknown component specifier %q", string(def.component))
			}

			if len(presMod) == 1 {
				def.presentation1 = presMod
			} else if len(presMod) > 1 {
				lastChar := presMod[len(presMod)-1]
				if strings.IndexByte("atco", lastChar) != -1 {
					def.presentation2 = lastChar
					if lastChar == 'o' {
						def.ordinal = true
					}
					def.presentation1 = presMod[:len(presMod)-1]
				} else {
					def.presentation1 = presMod
				}
			} else {
				def.presentation1 = defaultPresentationModifiers[def.component]
			}

			if len(def.presentation1) > 0 && def.presentation1[0] == 'n' {
				def.names = tcaseLower
			} else if len(def.presentation1) > 0 && def.presentation1[0] == 'N' {
				if len(def.presentation1) > 1 && def.presentation1[1] == 'n' {
					def.names = tcaseTitle
				} else {
					def.names = tcaseUpper
				}
			} else if strings.IndexByte("YMDdFWwXxHhmsf", def.component) != -1 {
				integerPattern := def.presentation1
				if def.presentation2 != 0 {
					integerPattern += ";" + string(def.presentation2)
				}
				intFmt, err := analyseIntegerPicture(integerPattern)
				if err != nil {
					return nil, err
				}
				def.integerFormat = intFmt
				if def.hasWidth && def.width.hasMin {
					if def.integerFormat.mandatoryDigits < def.width.min {
						def.integerFormat.mandatoryDigits = def.width.min
					}
				}
				if def.component == 'Y' {
					def.n = -1
					if def.hasWidth && def.width.hasMax {
						def.n = def.width.max
						def.integerFormat.mandatoryDigits = def.n
					} else {
						w := def.integerFormat.mandatoryDigits + def.integerFormat.optionalDigits
						if w >= 2 {
							def.n = w
						}
					}
				}
				if len(spec) > 0 && spec[len(spec)-1].integerFormat != nil {
					spec[len(spec)-1].integerFormat.parseWidth = spec[len(spec)-1].integerFormat.mandatoryDigits
				}
			}

			if def.component == 'Z' || def.component == 'z' {
				intFmt, err := analyseIntegerPicture(def.presentation1)
				if err != nil {
					return nil, err
				}
				def.integerFormat = intFmt
			}

			spec = append(spec, def)
			start = pos + 1
		}
		pos++
	}
	addLiteral(start, pos)
	return &dateTimeFormatSpec{parts: spec}, nil
}

var (
	dayNames   = []string{"", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"}
	monthNames = []string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
)

const millisInADay = 1000 * 60 * 60 * 24

type ymPair struct {
	year  int
	month int // 0-indexed (0 = Jan, 11 = Dec)
}

func (ym ymPair) nextMonth() ymPair {
	if ym.month == 11 {
		return ymPair{ym.year + 1, 0}
	}
	return ymPair{ym.year, ym.month + 1}
}

func (ym ymPair) previousMonth() ymPair {
	if ym.month == 0 {
		return ymPair{ym.year - 1, 11}
	}
	return ymPair{ym.year, ym.month - 1}
}

func (ym ymPair) nextYear() ymPair {
	return ymPair{ym.year + 1, ym.month}
}

func (ym ymPair) previousYear() ymPair {
	return ymPair{ym.year - 1, ym.month}
}

func dateUTCMillis(year, monthZeroIndexed, day int) int64 {
	t := time.Date(year, time.Month(monthZeroIndexed+1), day, 0, 0, 0, 0, time.UTC)
	return t.UnixNano() / int64(time.Millisecond)
}

func startOfFirstWeek(ym ymPair) int64 {
	jan1 := dateUTCMillis(ym.year, ym.month, 1)
	tJan1 := msToTime(jan1).UTC()
	dayOfJan1 := int(tJan1.Weekday())
	if dayOfJan1 == 0 {
		dayOfJan1 = 7
	}
	if dayOfJan1 > 4 {
		return jan1 + int64(8-dayOfJan1)*millisInADay
	}
	return jan1 - int64(dayOfJan1-1)*millisInADay
}

func deltaWeeks(start, end int64) float64 {
	return float64(end-start)/float64(millisInADay*7) + 1
}

func getDateTimeFragment(t time.Time, component byte) (int, string) {
	t = t.UTC()
	year := t.Year()
	month0 := int(t.Month()) - 1
	day := t.Day()
	nowMillis := t.UnixNano() / int64(time.Millisecond)

	switch component {
	case 'Y':
		return year, ""
	case 'M':
		return month0 + 1, ""
	case 'D':
		return day, ""
	case 'd':
		today := dateUTCMillis(year, month0, day)
		firstJan := dateUTCMillis(year, 0, 1)
		return int((today-firstJan)/millisInADay) + 1, ""
	case 'F':
		dow := int(t.Weekday())
		if dow == 0 {
			dow = 7
		}
		return dow, ""
	case 'W':
		thisYear := ymPair{year, 0}
		startOfWeek1 := startOfFirstWeek(thisYear)
		today := dateUTCMillis(thisYear.year, month0, day)
		week := deltaWeeks(startOfWeek1, today)
		if week > 52 {
			startOfFollowingYear := startOfFirstWeek(thisYear.nextYear())
			if today >= startOfFollowingYear {
				week = 1
			}
		} else if week < 1 {
			startOfPreviousYear := startOfFirstWeek(thisYear.previousYear())
			week = deltaWeeks(startOfPreviousYear, today)
		}
		return int(math.Floor(week)), ""
	case 'w':
		thisMonth := ymPair{year, month0}
		startOfWeek1 := startOfFirstWeek(thisMonth)
		today := dateUTCMillis(thisMonth.year, thisMonth.month, day)
		week := deltaWeeks(startOfWeek1, today)
		if week > 4 {
			startOfFollowingMonth := startOfFirstWeek(thisMonth.nextMonth())
			if today >= startOfFollowingMonth {
				week = 1
			}
		} else if week < 1 {
			startOfPreviousMonth := startOfFirstWeek(thisMonth.previousMonth())
			week = deltaWeeks(startOfPreviousMonth, today)
		}
		return int(math.Floor(week)), ""
	case 'X':
		thisYear := ymPair{year, 0}
		startOfISOYear := startOfFirstWeek(thisYear)
		endOfISOYear := startOfFirstWeek(thisYear.nextYear())
		if nowMillis < startOfISOYear {
			return thisYear.year - 1, ""
		} else if nowMillis >= endOfISOYear {
			return thisYear.year + 1, ""
		}
		return thisYear.year, ""
	case 'x':
		thisMonth := ymPair{year, month0}
		startOfISOMonth := startOfFirstWeek(thisMonth)
		nextM := thisMonth.nextMonth()
		endOfISOMonth := startOfFirstWeek(nextM)
		if nowMillis < startOfISOMonth {
			return thisMonth.previousMonth().month + 1, ""
		} else if nowMillis >= endOfISOMonth {
			return nextM.month + 1, ""
		}
		return thisMonth.month + 1, ""
	case 'H':
		return t.Hour(), ""
	case 'h':
		h := t.Hour() % 12
		if h == 0 {
			h = 12
		}
		return h, ""
	case 'P':
		if t.Hour() >= 12 {
			return 1, "pm"
		}
		return 0, "am"
	case 'm':
		return t.Minute(), ""
	case 's':
		return t.Second(), ""
	case 'f':
		return t.Nanosecond() / int(time.Millisecond), ""
	case 'C', 'E':
		return 0, "ISO"
	}
	return 0, ""
}

var reTZOffset = regexp.MustCompile(`^[+-][0-9]{4}$`)

// FromMillis formats milliseconds since the epoch to a formatted string.
func FromMillis(ms int64, picture jtypes.OptionalString, tz jtypes.OptionalString) (string, error) {
	offsetHours := 0
	offsetMinutes := 0

	if tz.String != "" && tz.String != "0000" {
		if !reTZOffset.MatchString(tz.String) {
			return "", fmt.Errorf("invalid timezone %q", tz.String)
		}
		offset, err := strconv.Atoi(tz.String)
		if err != nil {
			return "", fmt.Errorf("invalid timezone %q", tz.String)
		}
		offsetHours = offset / 100
		offsetMinutes = offset % 100
	}

	layout := picture.String
	if layout == "" {
		layout = defaultFormatTimeLayout
	}

	formatSpec, err := analyseDateTimePicture(layout)
	if err != nil {
		return "", err
	}

	offsetMillis := int64(60*offsetHours+offsetMinutes) * 60 * 1000
	dateTime := msToTime(ms + offsetMillis).UTC()

	var result strings.Builder
	for _, part := range formatSpec.parts {
		if part.partType == "literal" {
			result.WriteString(part.value)
		} else {
			compStr, err := formatDateTimeComponent(dateTime, part, offsetHours, offsetMinutes)
			if err != nil {
				return "", err
			}
			result.WriteString(compStr)
		}
	}

	return result.String(), nil
}

func formatDateTimeComponent(date time.Time, markerSpec *dateTimePart, offsetHours, offsetMinutes int) (string, error) {
	intVal, strVal := getDateTimeFragment(date, markerSpec.component)

	if strings.IndexByte("YMDdFWwXxHhms", markerSpec.component) != -1 {
		if markerSpec.component == 'Y' && markerSpec.n != -1 {
			mod := int(math.Pow10(markerSpec.n))
			if mod > 0 {
				intVal = intVal % mod
			}
		}
		if markerSpec.names != "" {
			var nameStr string
			switch markerSpec.component {
			case 'M', 'x':
				if intVal >= 1 && intVal <= 12 {
					nameStr = monthNames[intVal-1]
				}
			case 'F':
				if intVal >= 1 && intVal <= 7 {
					nameStr = dayNames[intVal]
				}
			default:
				return "", fmt.Errorf("D3133: The 'name' modifier cannot be applied to component %q", string(markerSpec.component))
			}
			switch markerSpec.names {
			case tcaseUpper:
				nameStr = strings.ToUpper(nameStr)
			case tcaseLower:
				nameStr = strings.ToLower(nameStr)
			}
			if markerSpec.hasWidth && markerSpec.width.hasMax && utf8.RuneCountInString(nameStr) > markerSpec.width.max {
				runes := []rune(nameStr)
				nameStr = string(runes[:markerSpec.width.max])
			}
			return nameStr, nil
		}
		return formatAnalyzedInteger(int64(intVal), markerSpec.integerFormat)
	} else if markerSpec.component == 'f' {
		return formatAnalyzedInteger(int64(intVal), markerSpec.integerFormat)
	} else if markerSpec.component == 'Z' || markerSpec.component == 'z' {
		offset := offsetHours*100 + offsetMinutes
		if markerSpec.names != "" && offset == 0 {
			return "UTC", nil
		}
		var compStr string
		var err error
		if markerSpec.integerFormat.regular {
			compStr, err = formatAnalyzedInteger(int64(offset), markerSpec.integerFormat)
			if err != nil {
				return "", err
			}
		} else {
			numDigits := markerSpec.integerFormat.mandatoryDigits
			switch numDigits {
			case 1, 2:
				compStr, err = formatAnalyzedInteger(int64(offsetHours), markerSpec.integerFormat)
				if err != nil {
					return "", err
				}
				if offsetMinutes != 0 {
					absMin := offsetMinutes
					if absMin < 0 {
						absMin = -absMin
					}
					minStr, err := FormatInteger(float64(absMin), "00")
					if err != nil {
						return "", err
					}
					compStr += ":" + minStr
				}
			case 3, 4:
				compStr, err = formatAnalyzedInteger(int64(offset), markerSpec.integerFormat)
				if err != nil {
					return "", err
				}
			default:
				return "", fmt.Errorf("D3134: The timezone integer format cannot have %d mandatory digits", numDigits)
			}
		}
		if offset >= 0 {
			compStr = "+" + compStr
		}
		if markerSpec.component == 'z' {
			compStr = "GMT" + compStr
		}
		if offset == 0 && markerSpec.presentation2 == 't' {
			compStr = "Z"
		}
		return compStr, nil
	} else if markerSpec.component == 'P' {
		if markerSpec.names == tcaseUpper {
			return strings.ToUpper(strVal), nil
		}
		return strVal, nil
	} else if markerSpec.component == 'C' || markerSpec.component == 'E' {
		return strVal, nil
	}

	return "", nil
}

type partMatcher struct {
	component byte
	regex     string
	parse     func(string) (int, error)
}

func generateIntMatcher(formatSpec *integerFormatSpec) (partMatcher, error) {
	var m partMatcher
	isUpper := formatSpec.tcase == tcaseUpper

	switch formatSpec.primary {
	case formatLetters:
		if isUpper {
			m.regex = "[A-Z]+"
		} else {
			m.regex = "[a-z]+"
		}
		m.parse = func(value string) (int, error) {
			aChar := rune('a')
			if isUpper {
				aChar = 'A'
			}
			return int(lettersToDecimal(strings.ToUpper(value), 'A')*0 + lettersToDecimalCase(value, aChar)), nil
		}
	case formatRomanNum:
		if isUpper {
			m.regex = "[MDCLXVI]+"
		} else {
			m.regex = "[mdclxvi]+"
		}
		m.parse = func(value string) (int, error) {
			return int(romanToDecimal(strings.ToUpper(value))), nil
		}
	case formatWordsNum:
		keys := make([]string, 0, len(wordValues)+2)
		for k := range wordValues {
			keys = append(keys, k)
		}
		keys = append(keys, "and")
		sort.Slice(keys, func(i, j int) bool {
			return len(keys[i]) > len(keys[j])
		})
		keys = append(keys, `[\\-, ]`)
		// Wait: in Go regexp, [\-,\s] or [\-, ]
		keys[len(keys)-1] = `[\-, ]`
		m.regex = "(?:" + strings.Join(keys, "|") + ")+"
		m.parse = func(value string) (int, error) {
			n, err := wordsToNumber(strings.ToLower(value))
			return int(n), err
		}
	case formatDecimal:
		m.regex = "[0-9]"
		if formatSpec.parseWidth > 0 {
			m.regex += fmt.Sprintf("{%d}", formatSpec.parseWidth)
		} else {
			m.regex += "+"
		}
		if formatSpec.ordinal {
			m.regex += "(?:th|st|nd|rd)"
		}
		m.parse = func(value string) (int, error) {
			digits := value
			if formatSpec.ordinal && len(digits) >= 2 {
				digits = digits[:len(digits)-2]
			}
			if formatSpec.regular {
				digits = strings.ReplaceAll(digits, ",", "")
			} else {
				for _, sep := range formatSpec.groupingSeparators {
					digits = strings.ReplaceAll(digits, sep.character, "")
				}
			}
			if formatSpec.zeroCode != '0' {
				var b strings.Builder
				for _, r := range digits {
					b.WriteRune(r - formatSpec.zeroCode + '0')
				}
				digits = b.String()
			}
			return strconv.Atoi(digits)
		}
	case formatSequence:
		return m, fmt.Errorf("D3130: The picture string %q is invalid", formatSpec.token)
	}
	return m, nil
}

func lettersToDecimalCase(letters string, aChar rune) float64 {
	if aChar == 'A' {
		letters = strings.ToUpper(letters)
	} else {
		letters = strings.ToLower(letters)
	}
	return lettersToDecimal(letters, aChar)
}

func generateDateTimeRegex(formatSpec *dateTimeFormatSpec) ([]partMatcher, error) {
	parts := make([]partMatcher, 0, len(formatSpec.parts))
	for _, part := range formatSpec.parts {
		var res partMatcher
		if part.partType == "literal" {
			res.regex = regexp.QuoteMeta(part.value)
		} else if part.component == 'Z' || part.component == 'z' {
			hasSep := part.integerFormat.regular && len(part.integerFormat.groupingSeparators) > 0
			var sepChar string
			if hasSep {
				sepChar = part.integerFormat.groupingSeparators[0].character
			}
			if part.component == 'z' {
				res.regex = "GMT"
			}
			res.regex += "[-+][0-9]+"
			if hasSep {
				res.regex += regexp.QuoteMeta(sepChar) + "[0-9]+"
			}
			comp := part.component
			res.parse = func(value string) (int, error) {
				if comp == 'z' && len(value) >= 3 {
					value = value[3:]
				}
				var offsetHours, offsetMinutes int
				if hasSep {
					idx := strings.Index(value, sepChar)
					offsetHours, _ = strconv.Atoi(value[:idx])
					offsetMinutes, _ = strconv.Atoi(value[idx+len(sepChar):])
				} else {
					numdigits := len(value) - 1
					if numdigits <= 2 {
						offsetHours, _ = strconv.Atoi(value)
					} else {
						offsetHours, _ = strconv.Atoi(value[:3])
						offsetMinutes, _ = strconv.Atoi(value[3:])
					}
				}
				if offsetHours < 0 && offsetMinutes > 0 {
					return offsetHours*60 - offsetMinutes, nil
				}
				return offsetHours*60 + offsetMinutes, nil
			}
		} else if part.component == 'f' {
			res.regex = "[0-9]+"
			res.parse = func(value string) (int, error) {
				sub := value
				if len(sub) > 3 {
					sub = sub[:3]
				}
				f, err := strconv.ParseFloat("0."+sub, 64)
				if err != nil {
					return 0, err
				}
				return int(f * 1000), nil
			}
		} else if part.integerFormat != nil {
			var err error
			res, err = generateIntMatcher(part.integerFormat)
			if err != nil {
				return nil, err
			}
		} else {
			res.regex = "[a-zA-Z]+"
			lookup := make(map[string]int)
			switch part.component {
			case 'M', 'x':
				for idx, name := range monthNames {
					key := name
					if part.hasWidth && part.width.hasMax && part.width.max > 0 && len(key) > part.width.max {
						key = key[:part.width.max]
					}
					lookup[strings.ToLower(key)] = idx + 1
				}
			case 'F':
				for idx, name := range dayNames {
					if idx > 0 {
						key := name
						if part.hasWidth && part.width.hasMax && part.width.max > 0 && len(key) > part.width.max {
							key = key[:part.width.max]
						}
						lookup[strings.ToLower(key)] = idx
					}
				}
			case 'P':
				lookup["am"] = 0
				lookup["pm"] = 1
			default:
				return nil, fmt.Errorf("D3133: The 'name' modifier cannot be applied to component %q", string(part.component))
			}
			res.parse = func(value string) (int, error) {
				v, ok := lookup[strings.ToLower(value)]
				if !ok {
					return 0, jtypes.ErrUndefined
				}
				return v, nil
			}
		}
		res.component = part.component
		parts = append(parts, res)
	}
	return parts, nil
}

func parseDateTimeWithPicture(timestamp, picture string) (int64, error) {
	formatSpec, err := analyseDateTimePicture(picture)
	if err != nil {
		return 0, err
	}
	matchParts, err := generateDateTimeRegex(formatSpec)
	if err != nil {
		return 0, err
	}

	var regexBuilder strings.Builder
	regexBuilder.WriteString("(?i)^")
	for _, p := range matchParts {
		regexBuilder.WriteByte('(')
		regexBuilder.WriteString(p.regex)
		regexBuilder.WriteByte(')')
	}
	regexBuilder.WriteByte('$')

	matcher, err := regexp.Compile(regexBuilder.String())
	if err != nil {
		return 0, err
	}

	info := matcher.FindStringSubmatch(timestamp)
	if info == nil {
		return 0, jtypes.ErrUndefined
	}

	const (
		dmA = 161 // 10100001
		dmB = 130 // 10000010
		dmC = 84  // 01010100
		dmD = 72  // 01001000
		tmA = 23  // 010111
		tmB = 47  // 101111
	)

	components := make(map[byte]int)
	for i := 1; i < len(info); i++ {
		mpart := matchParts[i-1]
		if mpart.parse != nil {
			val, err := mpart.parse(info[i])
			if err != nil {
				return 0, err
			}
			components[mpart.component] = val
		}
	}

	if len(components) == 0 {
		return 0, jtypes.ErrUndefined
	}

	mask := 0
	shift := func(bit bool) {
		mask <<= 1
		if bit {
			mask |= 1
		}
	}
	isType := func(t int) bool {
		return ((^t)&mask) == 0 && (t&mask) != 0
	}

	for _, part := range []byte("YXMxWwdD") {
		_, ok := components[part]
		shift(ok)
	}

	dateA := isType(dmA)
	dateB := !dateA && isType(dmB)
	dateC := isType(dmC)
	dateD := !dateC && isType(dmD)

	mask = 0
	for _, part := range []byte("PHhmsf") {
		_, ok := components[part]
		shift(ok)
	}

	timeA := isType(tmA)
	timeB := !timeA && isType(tmB)

	dateComps := "YMD"
	if dateB {
		dateComps = "Yd"
	} else if dateC {
		dateComps = "XxwF"
	} else if dateD {
		dateComps = "XWF"
	}

	timeComps := "Hmsf"
	if timeB {
		timeComps = "Phmsf"
	}

	comps := dateComps + timeComps
	now := time.Now().UTC()

	startSpecified := false
	endSpecified := false
	for i := 0; i < len(comps); i++ {
		part := comps[i]
		if _, ok := components[part]; !ok {
			if startSpecified {
				if strings.IndexByte("MDd", part) != -1 {
					components[part] = 1
				} else {
					components[part] = 0
				}
				endSpecified = true
			} else {
				v, _ := getDateTimeFragment(now, part)
				components[part] = v
			}
		} else {
			startSpecified = true
			if endSpecified {
				return 0, fmt.Errorf("D3136: The date/time picture string is underspecified")
			}
		}
	}

	if components['M'] > 0 {
		components['M']--
	} else {
		components['M'] = 0
	}

	if dateB {
		firstJan := time.Date(components['Y'], time.January, 1, 0, 0, 0, 0, time.UTC)
		derivedDate := firstJan.AddDate(0, 0, components['d']-1)
		components['M'] = int(derivedDate.Month()) - 1
		components['D'] = derivedDate.Day()
	}
	if dateC || dateD {
		return 0, fmt.Errorf("D3136: Parsing this date format is not currently supported")
	}
	if timeB {
		if components['h'] == 12 {
			components['H'] = 0
		} else {
			components['H'] = components['h']
		}
		if components['P'] == 1 {
			components['H'] += 12
		}
	}

	t := time.Date(
		components['Y'],
		time.Month(components['M']+1),
		components['D'],
		components['H'],
		components['m'],
		components['s'],
		components['f']*int(time.Millisecond),
		time.UTC,
	)
	millis := timeToMS(t)
	if tzMin, ok := components['Z']; ok {
		millis -= int64(tzMin) * 60 * 1000
	} else if tzMin, ok := components['z']; ok {
		millis -= int64(tzMin) * 60 * 1000
	}
	return millis, nil
}

var iso8601Regex = regexp.MustCompile(`^(\d{4})(?:-([01]\d))?(?:-([0-3]\d))?(?:T([0-2]\d):([0-5]\d):([0-5]\d))?(?:\.(\d+))?([+-][0-2]\d:?[0-5]\d|Z)?$`)

// ToMillis converts a timestamp string to milliseconds since the epoch.
func ToMillis(s string, picture jtypes.OptionalString, _ jtypes.OptionalString) (int64, error) {
	if picture.String == "" {
		m := iso8601Regex.FindStringSubmatch(s)
		if m == nil {
			return 0, fmt.Errorf("could not parse time %q", s)
		}
		year, _ := strconv.Atoi(m[1])
		month := 1
		if m[2] != "" {
			month, _ = strconv.Atoi(m[2])
		}
		day := 1
		if m[3] != "" {
			day, _ = strconv.Atoi(m[3])
		}
		hour, minute, second := 0, 0, 0
		if m[4] != "" {
			hour, _ = strconv.Atoi(m[4])
			minute, _ = strconv.Atoi(m[5])
			second, _ = strconv.Atoi(m[6])
		}
		millisPart := 0
		if m[7] != "" {
			sub := m[7]
			if len(sub) > 3 {
				sub = sub[:3]
			}
			f, _ := strconv.ParseFloat("0."+sub, 64)
			millisPart = int(math.Round(f * 1000))
		}
		tzOffsetMinutes := 0
		if m[8] != "" && m[8] != "Z" {
			tzStr := strings.ReplaceAll(m[8], ":", "")
			sign := 1
			if tzStr[0] == '-' {
				sign = -1
			}
			tzH, _ := strconv.Atoi(tzStr[1:3])
			tzM, _ := strconv.Atoi(tzStr[3:5])
			tzOffsetMinutes = sign * (tzH*60 + tzM)
		}
		t := time.Date(year, time.Month(month), day, hour, minute, second, millisPart*int(time.Millisecond), time.UTC)
		return timeToMS(t) - int64(tzOffsetMinutes)*60*1000, nil
	}

	return parseDateTimeWithPicture(s, picture.String)
}

func msToTime(ms int64) time.Time {
	return time.Unix(ms/1000, (ms%1000)*int64(time.Millisecond))
}

func timeToMS(t time.Time) int64 {
	return t.UnixNano() / int64(time.Millisecond)
}
