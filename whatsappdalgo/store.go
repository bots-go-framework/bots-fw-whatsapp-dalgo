// Package whatsappdalgo contains DALgo implementations of WhatsApp persistence
// ports. It preserves the existing collection names and key formats.
package whatsappdalgo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bots-go-framework/bots-fw-whatsapp/whatsapp"
	"github.com/dal-go/dalgo/dal"
	"github.com/dal-go/record"
)

// DBProvider returns the DALgo database for a request context.
type DBProvider func(context.Context) (dal.DB, error)

func fixedDB(db dal.DB) DBProvider {
	if db == nil {
		panic("db is required")
	}
	return func(context.Context) (dal.DB, error) { return db, nil }
}

func resolveDB(ctx context.Context, getDB DBProvider) (dal.DB, error) {
	db, err := getDB(ctx)
	if err != nil {
		return nil, fmt.Errorf("get DALgo database: %w", err)
	}
	if db == nil {
		return nil, errors.New("DALgo database provider returned nil")
	}
	return db, nil
}

const (
	subjectsCollection      = "waSubjects"
	chatDataCollection      = "waChatData"
	templatesCollection     = "waTemplates"
	templateNamesCollection = "waTemplateNames"
)

type subjectData struct {
	Subject   string    `firestore:"subject" json:"subject"`
	ExpiresAt time.Time `firestore:"expiresAt" json:"expiresAt"`
}

type SubjectStore struct{ getDB DBProvider }

var _ whatsapp.SubjectStore = (*SubjectStore)(nil)

func NewSubjectStore(db dal.DB) *SubjectStore { return NewSubjectStoreWithProvider(fixedDB(db)) }

func NewSubjectStoreWithProvider(getDB DBProvider) *SubjectStore {
	if getDB == nil {
		panic("getDB is required")
	}
	return &SubjectStore{getDB: getDB}
}

func subjectKey(botID, wamid string) string { return botID + ":" + wamid }

func (s *SubjectStore) PutSubject(ctx context.Context, botID, wamid, subject string, expiresAt time.Time) error {
	db, err := resolveDB(ctx, s.getDB)
	if err != nil {
		return err
	}
	key := record.NewKeyWithID(subjectsCollection, subjectKey(botID, wamid))
	data := &subjectData{Subject: subject, ExpiresAt: expiresAt}
	return db.RunReadwriteTransaction(ctx, func(ctx context.Context, tx dal.ReadwriteTransaction) error {
		return tx.Set(ctx, record.NewRecordWithData(key, data))
	})
}

func (s *SubjectStore) GetSubject(ctx context.Context, botID, wamid string) (string, bool, error) {
	db, err := resolveDB(ctx, s.getDB)
	if err != nil {
		return "", false, err
	}
	data := &subjectData{}
	if err := db.Get(ctx, record.NewRecordWithData(record.NewKeyWithID(subjectsCollection, subjectKey(botID, wamid)), data)); err != nil {
		if record.IsNotFound(err) {
			return "", false, nil
		}
		return "", false, err
	}
	if time.Now().After(data.ExpiresAt) {
		return "", false, nil
	}
	return data.Subject, true, nil
}

type ChatDataStore struct{ getDB DBProvider }

var _ whatsapp.ChatDataStore = (*ChatDataStore)(nil)

func NewChatDataStore(db dal.DB) *ChatDataStore { return NewChatDataStoreWithProvider(fixedDB(db)) }

func NewChatDataStoreWithProvider(getDB DBProvider) *ChatDataStore {
	if getDB == nil {
		panic("getDB is required")
	}
	return &ChatDataStore{getDB: getDB}
}

func chatDataKey(botID, chatID string) string { return botID + ":" + chatID }

func (s *ChatDataStore) GetChatData(ctx context.Context, botID, chatID string) (*whatsapp.WaChatData, error) {
	db, err := resolveDB(ctx, s.getDB)
	if err != nil {
		return nil, err
	}
	data := &whatsapp.WaChatData{}
	if err := db.Get(ctx, record.NewRecordWithData(record.NewKeyWithID(chatDataCollection, chatDataKey(botID, chatID)), data)); err != nil {
		if record.IsNotFound(err) {
			return &whatsapp.WaChatData{}, nil
		}
		return nil, err
	}
	return data, nil
}

func (s *ChatDataStore) SaveChatData(ctx context.Context, botID, chatID string, data *whatsapp.WaChatData) error {
	db, err := resolveDB(ctx, s.getDB)
	if err != nil {
		return err
	}
	key := record.NewKeyWithID(chatDataCollection, chatDataKey(botID, chatID))
	return db.RunReadwriteTransaction(ctx, func(ctx context.Context, tx dal.ReadwriteTransaction) error {
		return tx.Set(ctx, record.NewRecordWithData(key, data))
	})
}

type templateList struct {
	Defs []whatsapp.TemplateDef `firestore:"defs" json:"defs"`
}

type templateNameRecord struct {
	Purpose string `firestore:"purpose" json:"purpose"`
}

type TemplateCatalog struct{ getDB DBProvider }

var _ whatsapp.TemplateCatalog = (*TemplateCatalog)(nil)

func NewTemplateCatalog(db dal.DB) *TemplateCatalog {
	return NewTemplateCatalogWithProvider(fixedDB(db))
}

func NewTemplateCatalogWithProvider(getDB DBProvider) *TemplateCatalog {
	if getDB == nil {
		panic("getDB is required")
	}
	return &TemplateCatalog{getDB: getDB}
}

func (c *TemplateCatalog) Get(ctx context.Context, purpose, locale string) (whatsapp.TemplateDef, bool, error) {
	db, err := resolveDB(ctx, c.getDB)
	if err != nil {
		return whatsapp.TemplateDef{}, false, err
	}
	list := &templateList{}
	if err := db.Get(ctx, record.NewRecordWithData(record.NewKeyWithID(templatesCollection, purpose), list)); err != nil {
		if record.IsNotFound(err) {
			return whatsapp.TemplateDef{}, false, nil
		}
		return whatsapp.TemplateDef{}, false, err
	}
	return pickApproved(list.Defs, locale)
}

func (c *TemplateCatalog) Upsert(ctx context.Context, purpose string, def whatsapp.TemplateDef) error {
	db, err := resolveDB(ctx, c.getDB)
	if err != nil {
		return err
	}
	return db.RunReadwriteTransaction(ctx, func(ctx context.Context, tx dal.ReadwriteTransaction) error {
		key := record.NewKeyWithID(templatesCollection, purpose)
		list := &templateList{}
		if err := tx.Get(ctx, record.NewRecordWithData(key, list)); err != nil && !record.IsNotFound(err) {
			return err
		}
		for i, old := range list.Defs {
			if old.Name == def.Name && old.Locale == def.Locale {
				list.Defs[i] = def
				return c.setTemplateList(ctx, tx, key, list, def.Name, purpose)
			}
		}
		list.Defs = append(list.Defs, def)
		return c.setTemplateList(ctx, tx, key, list, def.Name, purpose)
	})
}

func (c *TemplateCatalog) setTemplateList(ctx context.Context, tx dal.ReadwriteTransaction, key *record.Key, list *templateList, name, purpose string) error {
	if err := tx.Set(ctx, record.NewRecordWithData(key, list)); err != nil {
		return err
	}
	return tx.Set(ctx, record.NewRecordWithData(record.NewKeyWithID(templateNamesCollection, name), &templateNameRecord{Purpose: purpose}))
}

func (c *TemplateCatalog) SetStatus(ctx context.Context, name string, status whatsapp.TemplateStatus) (found bool, err error) {
	db, err := resolveDB(ctx, c.getDB)
	if err != nil {
		return false, err
	}
	nameData := &templateNameRecord{}
	if err = db.Get(ctx, record.NewRecordWithData(record.NewKeyWithID(templateNamesCollection, name), nameData)); err != nil {
		if record.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	err = db.RunReadwriteTransaction(ctx, func(ctx context.Context, tx dal.ReadwriteTransaction) error {
		key := record.NewKeyWithID(templatesCollection, nameData.Purpose)
		list := &templateList{}
		if err := tx.Get(ctx, record.NewRecordWithData(key, list)); err != nil {
			if record.IsNotFound(err) {
				return nil
			}
			return err
		}
		for i := range list.Defs {
			if list.Defs[i].Name == name {
				list.Defs[i].Status = status
				found = true
				return tx.Set(ctx, record.NewRecordWithData(key, list))
			}
		}
		return nil
	})
	return found, err
}

func pickApproved(defs []whatsapp.TemplateDef, locale string) (whatsapp.TemplateDef, bool, error) {
	lang := locale
	if i := strings.IndexByte(locale, '_'); i >= 0 {
		lang = locale[:i]
	}
	var langMatch *whatsapp.TemplateDef
	var first *whatsapp.TemplateDef
	for i := range defs {
		def := &defs[i]
		if def.Status != whatsapp.TemplateStatusApproved {
			continue
		}
		if first == nil {
			first = def
		}
		if def.Locale == locale {
			return *def, true, nil
		}
		defLang := def.Locale
		if j := strings.IndexByte(defLang, '_'); j >= 0 {
			defLang = defLang[:j]
		}
		if langMatch == nil && defLang == lang {
			langMatch = def
		}
	}
	if langMatch != nil {
		return *langMatch, true, nil
	}
	if first != nil {
		return *first, true, nil
	}
	return whatsapp.TemplateDef{}, false, nil
}
