"""
Word matching of a reference text against heard words: the part of ai-trns's textdiff.py the markup needs
(copied; it is stdlib only). Both sides are normalized (niqqud, quotes, punctuation stripped); words at or above
`threshold` character similarity count as equal, so spelling variants (ktiv male / haser) still match.
"""
import re
from difflib import SequenceMatcher

FUZZY_THRESHOLD = 0.8
# Above this many cells, a replace block skips the fuzzy DP
_MAX_DP_CELLS = 1_000_000

_NIQQUD_RE = re.compile(r"[֑-ׇ]")
_QUOTE_RE = re.compile(r"[\"'׳״‘’“”]")
_PUNCT_RE = re.compile(r"[,.;:!?()]")


def _norm(token: str) -> str:
    """Niqqud, quotes and punctuation stripped; a token with no letters/digits left (dash, maqaf...) is not a word"""
    norm = _PUNCT_RE.sub("", _QUOTE_RE.sub("", _NIQQUD_RE.sub("", token)))
    return norm if any(c.isalnum() for c in norm) else ""


def _similar(a: str, b: str, threshold: float) -> bool:
    return a == b or SequenceMatcher(None, a, b).ratio() >= threshold


def _fuzzy_ops(ref: list[str], hyp: list[str], i0: int, j0: int, threshold: float) -> list[tuple]:
    """Edit-distance alignment of one replace block -> ops with absolute indices"""
    n, m = len(ref), len(hyp)
    if n * m > _MAX_DP_CELLS:
        return [("del", i0 + i) for i in range(n)] + [("ins", j0 + j) for j in range(m)]

    sim = [[_similar(ref[i], hyp[j], threshold) for j in range(m)] for i in range(n)]
    cost = [[0] * (m + 1) for _ in range(n + 1)]
    for i in range(1, n + 1):
        cost[i][0] = i
    for j in range(1, m + 1):
        cost[0][j] = j
    for i in range(1, n + 1):
        for j in range(1, m + 1):
            cost[i][j] = min(
                cost[i - 1][j - 1] + (0 if sim[i - 1][j - 1] else 1),
                cost[i - 1][j] + 1,
                cost[i][j - 1] + 1,
            )

    ops = []
    i, j = n, m
    while i > 0 or j > 0:
        if i > 0 and j > 0 and cost[i][j] == cost[i - 1][j - 1] + (0 if sim[i - 1][j - 1] else 1):
            ops.append(("eq" if sim[i - 1][j - 1] else "sub", i0 + i - 1, j0 + j - 1))
            i, j = i - 1, j - 1
        elif i > 0 and cost[i][j] == cost[i - 1][j] + 1:
            ops.append(("del", i0 + i - 1))
            i -= 1
        else:
            ops.append(("ins", j0 + j - 1))
            j -= 1
    ops.reverse()
    return ops


def _word_ops(ref: list[str], hyp: list[str], threshold: float) -> list[tuple]:
    ops = []
    matcher = SequenceMatcher(None, ref, hyp, autojunk=False)
    for tag, i1, i2, j1, j2 in matcher.get_opcodes():
        if tag == "equal":
            ops.extend(("eq", i1 + k, j1 + k) for k in range(i2 - i1))
        elif tag == "delete":
            ops.extend(("del", i) for i in range(i1, i2))
        elif tag == "insert":
            ops.extend(("ins", j) for j in range(j1, j2))
        else:
            ops.extend(_fuzzy_ops(ref[i1:i2], hyp[j1:j2], i1, j1, threshold))
    return ops
