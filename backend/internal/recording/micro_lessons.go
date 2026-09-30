package recording

// Lessons are curated application content, never generated during a card read.
var microLessons = map[string]MicroLesson{
	"subject-verb-agreement":         {"Согласование подлежащего и глагола", [3]string{"В Present Simple после he, she, it добавляйте -s: she works.", "После do и does используйте начальную форму: does she work?", "Согласуйте be с подлежащим: I am, you are, she is."}},
	"verb-forms":                     {"Формы глагола", [3]string{"Для завершённого прошлого используйте вторую форму: went, worked.", "После have используйте третью форму: have gone, have worked.", "После did возвращайте начальную форму: did go, didn't work."}},
	"present-simple-vs-continuous":   {"Привычка или действие сейчас", [3]string{"Привычки: I work every day.", "Действие сейчас: I am working now.", "С глаголами состояния обычно используйте Simple: I know, I want."}},
	"past-simple-vs-present-perfect": {"Прошлое и связь с настоящим", [3]string{"С завершённым временем используйте Past Simple: I went there yesterday.", "Для опыта без конкретного прошлого времени: I have been there.", "Present Perfect строится как have/has + третья форма глагола."}},
	"modal-verbs":                    {"Модальные глаголы", [3]string{"После can, must, should используйте глагол без to: can help.", "Не добавляйте -s: she can help.", "Can выражает возможность, must — необходимость, should — совет."}},
	"infinitive-vs-gerund":           {"Инфинитив и форма -ing", [3]string{"После want и decide используйте to: want to learn.", "После enjoy и avoid используйте -ing: enjoy learning.", "После предлога используйте -ing: interested in learning."}},
	"conditionals":                   {"Условные предложения", [3]string{"Реальная возможность: If I have time, I will help.", "Гипотетическая ситуация: If I had time, I would help.", "Другое прошлое: If I had known, I would have helped."}},
	"articles-a-an-the":              {"Артикли a, an и the", [3]string{"A/an вводит один предмет: I bought a book.", "The указывает на уже понятный предмет: the book you gave me.", "Выбирайте a/an по звуку: a university, an hour."}},
	"zero-article":                   {"Когда артикль не нужен", [3]string{"Говорите без артикля о вещах в целом: Books are useful.", "Неисчисляемое в общем смысле: I like music.", "Для конкретного предмета артикль может понадобиться: the music in this film."}},
	"countable-vs-uncountable":       {"Исчисляемые и неисчисляемые слова", [3]string{"Исчисляемые слова имеют единственное и множественное число: a job, jobs.", "Information и advice обычно неисчисляемые: some advice.", "Для отдельной единицы используйте a piece of: a piece of advice."}},
	"singular-and-plural-nouns":      {"Единственное и множественное число", [3]string{"После one используйте единственное число, после two — множественное.", "Обычно добавляйте -s: book → books.", "Запоминайте исключения: child → children, person → people."}},
	"quantifiers":                    {"Слова количества", [3]string{"С исчисляемыми: many books, a few books.", "С неисчисляемыми: much time, a little time.", "A lot of подходит для обоих типов: a lot of books/time."}},
	"prepositions-of-time-and-place": {"Предлоги времени и места", [3]string{"Время: at five, on Monday, in September.", "Место: at the station, on the table, in the room.", "Учите выражения целиком: at home, at night, in the morning."}},
	"dependent-prepositions":         {"Предлоги после слов", [3]string{"Учите глагол вместе с предлогом: listen to, depend on.", "После прилагательных тоже бывают предлоги: interested in, good at.", "После предлога перед глаголом используйте -ing: good at explaining."}},
	"word-formation":                 {"Выбор формы слова", [3]string{"Для предмета или понятия нужно существительное: success.", "Для описания существительного — прилагательное: successful work.", "Для описания действия часто нужно наречие: work successfully."}},
	"collocations":                   {"Естественные сочетания", [3]string{"Запоминайте сочетание целиком: make a decision.", "Проверяйте глагол рядом с существительным: do homework.", "Повторяйте новое сочетание в своём коротком предложении."}},
	"false-friends":                  {"Похожие слова с разным смыслом", [3]string{"Не переводите знакомое слово только по сходству звучания.", "Actually обычно значит «на самом деле», а currently — «сейчас».", "Проверяйте значение по контексту и учите собственный пример."}},
	"word-order":                     {"Порядок слов", [3]string{"Базовый порядок: кто → действие → объект: I like this book.", "Прилагательное обычно стоит перед существительным: an interesting book.", "В вопросах часто нужен вспомогательный глагол: Do you like it?"}},
	"questions-and-negatives":        {"Вопросы и отрицания", [3]string{"В Present Simple используйте do/does: Do you work?", "После did нужен глагол в начальной форме: Did you go?", "С be меняйте порядок: Are you ready?; отрицание — aren't ready."}},
	"sentence-fragments-and-run-ons": {"Связные предложения", [3]string{"Проверьте, что главная мысль содержит подлежащее и сказуемое.", "Связывайте мысли по смыслу: and, but, because, so.", "В устной речи короткий ответ допустим, если вопрос делает его понятным."}},
	"relative-clauses":               {"Уточняющие придаточные", [3]string{"Для людей часто используйте who: a friend who helps me.", "Для вещей — which или that: a book that I like.", "Следите, чтобы уточнение явно относилось к нужному слову."}},
	"pronoun-reference":              {"Понятные местоимения", [3]string{"Должно быть ясно, кого или что заменяет it, they, he или she.", "Согласуйте число: a book → it, books → they.", "Если рядом несколько возможных объектов, повторите нужное существительное."}},
	"english-fillers":                {"Паузы на английском", [3]string{"Чтобы подумать, скажите well или let me think.", "Чтобы уточнить мысль, используйте I mean или how can I put it?", "После короткой паузы продолжите свою основную мысль."}},
	"expressing-ideas-in-english":    {"Как выразить мысль на английском", [3]string{"Если забыли слово, опишите его простыми английскими словами.", "Используйте it is something that… или it is used for….", "Запомните нужное слово вместе с предложением из своего ответа."}},
}

func LessonFor(ruleID string) *MicroLesson {
	lesson, exists := microLessons[ruleID]
	if !exists {
		return nil
	}
	return &lesson
}
