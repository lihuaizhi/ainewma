export function normalizeText(s) {
    if (!s)
        return "";
    return s
        .toString()
        .trim()
        .toLowerCase()
        .replace(/\u3000/g, " ");
}
export function containsMatch(haystack, needle) {
    if (!needle)
        return true;
    if (!haystack)
        return false;
    return haystack.includes(needle);
}
export function toStringArray(v) {
    if (!v)
        return [];
    if (Array.isArray(v)) {
        return v
            .map((x) => (x == null ? "" : String(x).trim()))
            .filter((x) => x.length > 0);
    }
    const s = String(v).trim();
    return s.length > 0 ? [s] : [];
}
export function buildHaystack(fields) {
    const parts = [];
    for (const f of fields) {
        if (!f)
            continue;
        if (Array.isArray(f)) {
            for (const x of f) {
                if (x)
                    parts.push(normalizeText(x));
            }
        }
        else {
            parts.push(normalizeText(f));
        }
    }
    return parts.join(" ");
}
export function scoreRelevance(haystack, needle) {
    if (!needle || !haystack)
        return 0;
    const h = normalizeText(haystack);
    const n = normalizeText(needle);
    if (n.length === 0)
        return 0;
    let score = 0;
    if (h === n) {
        score += 60;
    }
    if (h.startsWith(n)) {
        score += 40;
    }
    const tokens = n.split(/[\s,，;；、/]+/).filter((t) => t.length > 0);
    if (tokens.length > 1) {
        for (const t of tokens) {
            if (h.includes(t))
                score += 15;
        }
    }
    else {
        if (h.includes(n)) {
            score += 20;
            score += 10;
        }
    }
    if (score > 100)
        score = 100;
    return score;
}
//# sourceMappingURL=search.js.map