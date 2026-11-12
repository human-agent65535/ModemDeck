// A small CSS reader for contract tests: it returns every rule with the
// at-rule conditions (@media, @container, @supports) it sits inside, so tests
// can assert what a selector resolves to instead of matching exact source text.

export function styleBlocks(sfc) {
  return [...sfc.matchAll(/<style[^>]*>([\s\S]*?)<\/style>/g)].map(match => match[1])
}

export function cssRules(css) {
  const text = css.replace(/\/\*[\s\S]*?\*\//g, '')
  const rules = []
  let index = 0

  function parse(conditions) {
    while (index < text.length) {
      const open = text.indexOf('{', index)
      const close = text.indexOf('}', index)
      if (close >= 0 && (open < 0 || close < open)) {
        index = close + 1
        return
      }
      if (open < 0) {
        index = text.length
        return
      }
      const prelude = text.slice(index, open).trim()
      index = open + 1
      if (prelude.startsWith('@')) {
        parse([...conditions, prelude])
        continue
      }
      const end = text.indexOf('}', index)
      const body = text.slice(index, end)
      index = end + 1
      rules.push({
        selectors: prelude.split(',').map(selector => selector.trim().replace(/\s+/g, ' ')),
        conditions,
        declarations: declarations(body)
      })
    }
  }

  parse([])
  return rules
}

function declarations(body) {
  const result = []
  for (const part of splitTopLevel(body, ';')) {
    const colon = part.indexOf(':')
    if (colon < 0) continue
    result.push({
      property: part.slice(0, colon).trim(),
      value: part.slice(colon + 1).trim().replace(/\s+/g, ' ')
    })
  }
  return result
}

// Splits on a separator outside parentheses, so `max(a, b)` stays one value.
export function splitTopLevel(value, separator) {
  const parts = []
  let depth = 0
  let current = ''
  for (const char of value) {
    if (char === '(') depth += 1
    if (char === ')') depth -= 1
    const split = separator === ' ' ? /\s/.test(char) : char === separator
    if (split && depth === 0) {
      if (current.trim()) parts.push(current.trim())
      current = ''
    } else {
      current += char
    }
  }
  if (current.trim()) parts.push(current.trim())
  return parts
}

/** The bottom edge of a `padding` shorthand, or of a later `padding-bottom`. */
export function paddingBottom(rule) {
  let bottom
  for (const { property, value } of rule.declarations) {
    if (property === 'padding-bottom' || property === 'padding-block-end') bottom = value
    if (property === 'padding-block') {
      const parts = splitTopLevel(value, ' ')
      bottom = parts[1] ?? parts[0]
    }
    if (property === 'padding') {
      const parts = splitTopLevel(value, ' ')
      bottom = parts.length >= 3 ? parts[2] : parts[0]
    }
  }
  return bottom
}

export function rulesFor(css, selector) {
  return cssRules(css).filter(rule => rule.selectors.includes(selector))
}

export function declaration(rule, property) {
  return rule.declarations.findLast(item => item.property === property)?.value
}
