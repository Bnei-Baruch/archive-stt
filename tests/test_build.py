"""Lesson markup logic (app.build): chapters by the transcript's marks, paragraphs timed by the heard words"""
from app import build

PARAS = [
    "",
    "שיעור בוקר - אחרי עריכה",
    "קריין: שיעור בנושא חוק הערבות.",
    "נמשיך אותו כי אנחנו צריכים חיבור בינינו",
    "קריין: קטע מס' 46 מתוך חוק הערבות.",
    '"כל ישראל יש להם חלק לעולם הבא"',
    "(הרב\"ש. מאמר 3)",
    "תתאר לעצמך שאתה הולך להתחתן עם החברים",
    "שאלה: מה זו אותה מערכת שקושרת בין דברים?",
    "זו המערכת של הבורא שקושרת הכול",
    "תלמיד: ולמה דווקא הבורא?",
    "כי הוא המקור של הכול",
    "קריין: קטע הבא, מס' 47, מאור ושמש.",
    '"העיקר הוא שכל אחד ואחד אל יחשוב לעצמו"',
    "מי שעושה מעצמו אפס בחברה מקשר את כולם",
]


def test_chapters_by_the_transcript_marks():
    chapters = build.marked_chapters(PARAS)
    assert [(c["type"], c["p_start"], c["p_end"]) for c in chapters] == [
        ("other", 1, 3), ("reading", 4, 6), ("commentary", 7, 7), ("qa", 8, 11), ("reading", 12, 13),
        ("commentary", 14, 14)]
    # the reader's announcement as written, not a parsed number; the commentary carries its reading's
    assert chapters[1]["title"] == chapters[2]["title"] == "קטע מס' 46 מתוך חוק הערבות."
    assert chapters[3]["title"] == "מה זו אותה מערכת שקושרת בין דברים?"
    assert all("item" not in c for c in chapters)
    assert build.marked_chapters(["רק טקסט", "בלי סימנים"]) is None


def test_items_numbered_any_way_and_students_without_question_marks():
    paras = ["קריין: \"תלמוד עשר הספירות\", חלק שני.", "קריין: אות ט\"ו.", '"טקסט האות"', "הסבר של הרב",
             "תלמיד: שאלה ראשונה?", "תשובה", "תלמיד: שאלה שנייה?", "תלמיד: והמשך שלה?", "תשובה"]
    chapters = build.marked_chapters(paras)
    assert [(c["type"], c["p_start"]) for c in chapters] == [
        ("reading", 0), ("commentary", 3), ("qa", 4), ("qa", 6)]
    assert chapters[0]["title"] == chapters[1]["title"] == '"תלמוד עשר הספירות", חלק שני. · אות ט"ו.'


def spoken(paras, skip=(), gap=0.3):
    """The heard words of the paragraphs, in order, without the marks and the paragraphs in `skip`"""
    words, t = [], 0.0
    for i, p in enumerate(paras):
        if i in skip:
            t += 5
            continue
        label = build.READER if p.startswith(("קריין", '"', "(")) else build.STUDENT if p.startswith(
            ("שאלה", "תלמיד")) else build.RAV
        for tok in build._MARK_RE.sub("", p).split():
            words.append({"word": tok, "start": t, "end": t + 0.25, "label": label, "reading": False})
            t += gap
    return words


def test_paragraphs_get_their_times_from_the_heard_words():
    words = spoken(PARAS, skip={1})  # the header is not spoken
    paragraphs, chapters, stats = build.from_transcript(PARAS, words, duration=60.0)
    at = {p["i"]: p for p in paragraphs}
    assert stats == {"transcript_paragraphs": 14, "aligned_paragraphs": 13}
    assert at[4]["start"] < at[5]["start"] < at[7]["start"] < at[8]["start"]
    assert at[1]["start"] == 0.0 and at[1]["end"] <= at[2]["start"]  # not heard: the gap before the next one
    assert [at[i]["speaker"] for i in (2, 5, 7, 8, 9, 10)] == [build.READER, build.READER, build.RAV,
                                                              build.STUDENT, build.RAV, build.STUDENT]
    # a chapter runs until the next one; the first from 0, the last to the end of the media
    assert chapters[0]["start"] == 0.0 and chapters[-1]["end"] == 60.0
    assert all(a["end"] == b["start"] for a, b in zip(chapters, chapters[1:]))
    assert chapters[1]["start"] == at[4]["start"]


def test_a_repeated_phrase_does_not_move_a_paragraph_back():
    paras = ["קריין: קטע מס' 1.", '"אחד שתיים שלוש ארבע"', "הסבר ארוך על הקטע הזה", "שאלה: אחד שתיים שלוש ארבע?"]
    words = spoken(paras[:3])
    spans = build.align(paras, words)
    assert spans[3] is None or spans[3][0] >= spans[2][0]


def test_chapters_from_the_audio_without_a_transcript():
    ps = [{"label": "קריין", "reading": False, "start": 0, "end": 5, "text": "קטע מס 3", "words": []},
          {"label": "קריין", "reading": True, "start": 5, "end": 30, "text": "טקסט המקור", "words": []},
          {"label": "הרב", "reading": False, "start": 30, "end": 90, "text": "הסבר", "words": []},
          {"label": "תלמיד", "reading": False, "start": 90, "end": 95, "text": "למה?", "words": []},
          {"label": "הרב", "reading": False, "start": 95, "end": 120, "text": "כי", "words": []},
          {"label": "תלמיד", "reading": False, "start": 120, "end": 125, "text": "ועוד?", "words": []}]
    paragraphs, chapters, _ = build.from_audio(ps, 130.0)
    assert [(c["type"], c["p_start"], c["p_end"]) for c in chapters] == [
        ("other", 0, 0), ("reading", 1, 1), ("commentary", 2, 2), ("qa", 3, 4), ("qa", 5, 5)]
    assert paragraphs[3] == {"i": 3, "start": 90, "end": 95, "speaker": "תלמיד", "text": "למה?"}


def test_markup_document_falls_back_to_the_audio_without_marks():
    ps = [{"label": "הרב", "reading": False, "start": 0, "end": 5, "text": "שלום",
           "words": [{"word": "שלום", "start": 0, "end": 1}]}]
    doc = build.markup("Unit0001", "he", {"file_uid": "Audio001", "duration_s": 10}, ps, "<p>בלי סימנים</p>",
                       {"file_uid": "Text0001"}, {"id": "r1"})
    assert doc["run"]["source"] == "audio" and doc["transcript"] is None
    assert [c["type"] for c in doc["chapters"]] == ["other"]
