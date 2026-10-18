package whatsappdalgo

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bots-go-framework/bots-fw-whatsapp/whatsapp"
	"github.com/dal-go/dalgo/adapters/dalgo2memory"
	"github.com/dal-go/dalgo/dal"
	"github.com/dal-go/record"
)

type errorDB struct {
	dal.DB
	getErr error
	txErr  error
}

func (e *errorDB) Get(ctx context.Context, record record.Record) error {
	if e.getErr != nil {
		return e.getErr
	}
	return e.DB.Get(ctx, record)
}

func (e *errorDB) RunReadwriteTransaction(ctx context.Context, f dal.RWTxWorker, options ...dal.TransactionOption) error {
	if e.txErr != nil {
		return e.txErr
	}
	return e.DB.RunReadwriteTransaction(ctx, f, options...)
}

type errorTx struct {
	dal.ReadwriteTransaction
	getErr error
	setErr error
}

func (tx *errorTx) Get(ctx context.Context, record record.Record) error {
	if tx.getErr != nil {
		return tx.getErr
	}
	return tx.ReadwriteTransaction.Get(ctx, record)
}

func (tx *errorTx) Set(ctx context.Context, record record.Record) error {
	if tx.setErr != nil {
		return tx.setErr
	}
	return tx.ReadwriteTransaction.Set(ctx, record)
}

type txInterceptDB struct {
	dal.DB
	txWrapper func(dal.ReadwriteTransaction) dal.ReadwriteTransaction
}

func (t *txInterceptDB) RunReadwriteTransaction(ctx context.Context, f dal.RWTxWorker, options ...dal.TransactionOption) error {
	return t.DB.RunReadwriteTransaction(ctx, func(ctx context.Context, tx dal.ReadwriteTransaction) error {
		if t.txWrapper != nil {
			tx = t.txWrapper(tx)
		}
		return f(ctx, tx)
	}, options...)
}

func TestDBResolution(t *testing.T) {
	ctx := context.Background()

	// Panics on nil DB in fixedDB
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic on nil DB in fixedDB")
			}
		}()
		fixedDB(nil)
	}()

	// resolveDB provider error
	customErr := errors.New("provider failed")
	_, err := resolveDB(ctx, func(context.Context) (dal.DB, error) {
		return nil, customErr
	})
	if !errors.Is(err, customErr) {
		t.Errorf("expected customErr, got %v", err)
	}

	// resolveDB provider returns nil db without error
	_, err = resolveDB(ctx, func(context.Context) (dal.DB, error) {
		return nil, nil
	})
	if err == nil || err.Error() != "DALgo database provider returned nil" {
		t.Errorf("expected 'DALgo database provider returned nil', got %v", err)
	}

	// Store constructors panic on nil provider
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic on nil provider in NewSubjectStoreWithProvider")
			}
		}()
		NewSubjectStoreWithProvider(nil)
	}()

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic on nil provider in NewChatDataStoreWithProvider")
			}
		}()
		NewChatDataStoreWithProvider(nil)
	}()

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic on nil provider in NewTemplateCatalogWithProvider")
			}
		}()
		NewTemplateCatalogWithProvider(nil)
	}()
}

func TestStores_Success(t *testing.T) {
	ctx := context.Background()
	memDB := dalgo2memory.New(dalgo2memory.FirestoreProfile())

	subjects := NewSubjectStore(memDB)
	if err := subjects.PutSubject(ctx, "bot-1", "wamid-1", "invoice", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("PutSubject() error: %v", err)
	}
	if subject, found, err := subjects.GetSubject(ctx, "bot-1", "wamid-1"); err != nil || !found || subject != "invoice" {
		t.Fatalf("GetSubject() = (%q, %v, %v)", subject, found, err)
	}

	chats := NewChatDataStore(memDB)
	chatData := &whatsapp.WaChatData{RecentInboundIDs: []string{"wamid-1"}}
	chatData.BotUserIDs = []string{"wa-user-1"}
	if err := chats.SaveChatData(ctx, "bot-1", "chat-1", chatData); err != nil {
		t.Fatalf("SaveChatData() error: %v", err)
	}
	chat, err := chats.GetChatData(ctx, "bot-1", "chat-1")
	if err != nil || len(chat.RecentInboundIDs) != 1 {
		t.Fatalf("GetChatData() = (%#v, %v)", chat, err)
	}
}

func TestSubjectStore(t *testing.T) {
	ctx := context.Background()
	memDB := dalgo2memory.New(dalgo2memory.FirestoreProfile())

	t.Run("resolveDB_error", func(t *testing.T) {
		store := NewSubjectStoreWithProvider(func(context.Context) (dal.DB, error) {
			return nil, errors.New("fail")
		})
		if err := store.PutSubject(ctx, "b", "w", "s", time.Now()); err == nil {
			t.Error("expected error from PutSubject")
		}
		if _, _, err := store.GetSubject(ctx, "b", "w"); err == nil {
			t.Error("expected error from GetSubject")
		}
	})

	t.Run("not_found", func(t *testing.T) {
		store := NewSubjectStore(memDB)
		subj, found, err := store.GetSubject(ctx, "b", "nonexistent")
		if err != nil || found || subj != "" {
			t.Errorf("expected not found, got (%q, %v, %v)", subj, found, err)
		}
	})

	t.Run("expired", func(t *testing.T) {
		store := NewSubjectStore(memDB)
		if err := store.PutSubject(ctx, "b", "expired-wamid", "subj", time.Now().Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
		subj, found, err := store.GetSubject(ctx, "b", "expired-wamid")
		if err != nil || found || subj != "" {
			t.Errorf("expected expired to return false, got (%q, %v, %v)", subj, found, err)
		}
	})

	t.Run("db_get_error", func(t *testing.T) {
		errDB := &errorDB{DB: memDB, getErr: errors.New("read error")}
		store := NewSubjectStore(errDB)
		_, _, err := store.GetSubject(ctx, "b", "wamid")
		if err == nil || err.Error() != "read error" {
			t.Errorf("expected 'read error', got %v", err)
		}
	})
}

func TestChatDataStore(t *testing.T) {
	ctx := context.Background()
	memDB := dalgo2memory.New(dalgo2memory.FirestoreProfile())

	t.Run("resolveDB_error", func(t *testing.T) {
		store := NewChatDataStoreWithProvider(func(context.Context) (dal.DB, error) {
			return nil, errors.New("fail")
		})
		if _, err := store.GetChatData(ctx, "b", "c"); err == nil {
			t.Error("expected error from GetChatData")
		}
		if err := store.SaveChatData(ctx, "b", "c", &whatsapp.WaChatData{}); err == nil {
			t.Error("expected error from SaveChatData")
		}
	})

	t.Run("not_found", func(t *testing.T) {
		store := NewChatDataStore(memDB)
		data, err := store.GetChatData(ctx, "b", "nonexistent")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if data == nil {
			t.Fatal("expected empty WaChatData, got nil")
		}
	})

	t.Run("db_get_error", func(t *testing.T) {
		errDB := &errorDB{DB: memDB, getErr: errors.New("read error")}
		store := NewChatDataStore(errDB)
		_, err := store.GetChatData(ctx, "b", "c")
		if err == nil || err.Error() != "read error" {
			t.Errorf("expected 'read error', got %v", err)
		}
	})

	t.Run("with_clock_and_nil_clock", func(t *testing.T) {
		fixedTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
		store := NewChatDataStore(memDB)
		store.WithClock(func() time.Time { return fixedTime })
		store.WithClock(nil) // nil is ignored

		data := &whatsapp.WaChatData{}
		data.BotUserIDs = []string{"user1"}
		if err := store.SaveChatData(ctx, "b", "c1", data); err != nil {
			t.Fatal(err)
		}
		if !data.DtCreated.Equal(fixedTime) {
			t.Errorf("expected DtCreated == fixedTime, got %v", data.DtCreated)
		}

		// Now force store.now = nil to test default time.Now
		store.now = nil
		data2 := &whatsapp.WaChatData{}
		data2.BotUserIDs = []string{"user1"}
		if err := store.SaveChatData(ctx, "b", "c2", data2); err != nil {
			t.Fatal(err)
		}
		if data2.DtCreated.IsZero() {
			t.Error("expected non-zero DtCreated")
		}
	})
}

func TestTemplateCatalog(t *testing.T) {
	ctx := context.Background()
	memDB := dalgo2memory.New(dalgo2memory.FirestoreProfile())

	t.Run("resolveDB_error", func(t *testing.T) {
		store := NewTemplateCatalogWithProvider(func(context.Context) (dal.DB, error) {
			return nil, errors.New("fail")
		})
		if _, _, err := store.Get(ctx, "p", "l"); err == nil {
			t.Error("expected error from Get")
		}
		if err := store.Upsert(ctx, "p", whatsapp.TemplateDef{}); err == nil {
			t.Error("expected error from Upsert")
		}
		if _, err := store.SetStatus(ctx, "n", whatsapp.TemplateStatusApproved); err == nil {
			t.Error("expected error from SetStatus")
		}
	})

	t.Run("get_not_found", func(t *testing.T) {
		cat := NewTemplateCatalog(memDB)
		def, found, err := cat.Get(ctx, "nonexistent", "en")
		if err != nil || found || def.Name != "" {
			t.Errorf("expected not found, got (%v, %v, %v)", def, found, err)
		}
	})

	t.Run("get_read_error", func(t *testing.T) {
		errDB := &errorDB{DB: memDB, getErr: errors.New("read error")}
		cat := NewTemplateCatalog(errDB)
		_, _, err := cat.Get(ctx, "p", "en")
		if err == nil || err.Error() != "read error" {
			t.Errorf("expected 'read error', got %v", err)
		}
	})

	t.Run("upsert_update_existing", func(t *testing.T) {
		cat := NewTemplateCatalog(memDB)
		def1 := whatsapp.TemplateDef{Name: "tpl1", Locale: "en_US", Status: whatsapp.TemplateStatusPending}
		if err := cat.Upsert(ctx, "welcome", def1); err != nil {
			t.Fatal(err)
		}
		// Update existing
		def2 := whatsapp.TemplateDef{Name: "tpl1", Locale: "en_US", Status: whatsapp.TemplateStatusApproved}
		if err := cat.Upsert(ctx, "welcome", def2); err != nil {
			t.Fatal(err)
		}
		got, found, err := cat.Get(ctx, "welcome", "en_US")
		if err != nil || !found || got.Status != whatsapp.TemplateStatusApproved {
			t.Errorf("expected updated status, got (%v, %v, %v)", got, found, err)
		}
	})

	t.Run("upsert_get_tx_error", func(t *testing.T) {
		txDB := &txInterceptDB{
			DB: memDB,
			txWrapper: func(tx dal.ReadwriteTransaction) dal.ReadwriteTransaction {
				return &errorTx{ReadwriteTransaction: tx, getErr: errors.New("tx get failed")}
			},
		}
		cat := NewTemplateCatalog(txDB)
		err := cat.Upsert(ctx, "welcome", whatsapp.TemplateDef{Name: "tpl_err"})
		if err == nil || err.Error() != "tx get failed" {
			t.Errorf("expected 'tx get failed', got %v", err)
		}
	})

	t.Run("upsert_set_tx_error", func(t *testing.T) {
		txDB := &txInterceptDB{
			DB: memDB,
			txWrapper: func(tx dal.ReadwriteTransaction) dal.ReadwriteTransaction {
				return &errorTx{ReadwriteTransaction: tx, setErr: errors.New("tx set failed")}
			},
		}
		cat := NewTemplateCatalog(txDB)
		err := cat.Upsert(ctx, "welcome", whatsapp.TemplateDef{Name: "tpl_err2"})
		if err == nil || err.Error() != "tx set failed" {
			t.Errorf("expected 'tx set failed', got %v", err)
		}
	})

	t.Run("set_status", func(t *testing.T) {
		cat := NewTemplateCatalog(memDB)

		// Name not found in templateNamesCollection
		found, err := cat.SetStatus(ctx, "unknown_tpl", whatsapp.TemplateStatusApproved)
		if err != nil || found {
			t.Errorf("expected false, nil for unknown, got (%v, %v)", found, err)
		}

		// DB error on reading name
		errDB := &errorDB{DB: memDB, getErr: errors.New("read name failed")}
		catErr := NewTemplateCatalog(errDB)
		_, err = catErr.SetStatus(ctx, "unknown_tpl", whatsapp.TemplateStatusApproved)
		if err == nil || err.Error() != "read name failed" {
			t.Errorf("expected 'read name failed', got %v", err)
		}

		// Add template then change status
		if err := cat.Upsert(ctx, "billing", whatsapp.TemplateDef{Name: "bill_v1", Locale: "en", Status: whatsapp.TemplateStatusPending}); err != nil {
			t.Fatal(err)
		}
		found, err = cat.SetStatus(ctx, "bill_v1", whatsapp.TemplateStatusApproved)
		if err != nil || !found {
			t.Errorf("SetStatus() = (%v, %v), want (true, nil)", found, err)
		}

		// SetStatus when templatesCollection record is missing or not found in list
		// 1) list missing
		if err := memDB.RunReadwriteTransaction(ctx, func(ctx context.Context, tx dal.ReadwriteTransaction) error {
			return tx.Delete(ctx, record.NewKeyWithID(templatesCollection, "billing"))
		}); err != nil {
			t.Fatal(err)
		}
		found, err = cat.SetStatus(ctx, "bill_v1", whatsapp.TemplateStatusApproved)
		if err != nil || found {
			t.Errorf("expected (false, nil) when list missing, got (%v, %v)", found, err)
		}

		// 2) list exists but doesn't have the name
		if err := cat.Upsert(ctx, "shipping", whatsapp.TemplateDef{Name: "ship_v1", Locale: "en"}); err != nil {
			t.Fatal(err)
		}
		// Manually point bill_v2's purpose to shipping
		if err := memDB.RunReadwriteTransaction(ctx, func(ctx context.Context, tx dal.ReadwriteTransaction) error {
			return tx.Set(ctx, record.NewRecordWithData(record.NewKeyWithID(templateNamesCollection, "bill_v2"), &templateNameRecord{Purpose: "shipping"}))
		}); err != nil {
			t.Fatal(err)
		}
		found, err = cat.SetStatus(ctx, "bill_v2", whatsapp.TemplateStatusApproved)
		if err != nil || found {
			t.Errorf("expected (false, nil) when name not in list, got (%v, %v)", found, err)
		}

		// 3) tx.Get error in SetStatus
		txDB := &txInterceptDB{
			DB: memDB,
			txWrapper: func(tx dal.ReadwriteTransaction) dal.ReadwriteTransaction {
				return &errorTx{ReadwriteTransaction: tx, getErr: errors.New("status tx get failed")}
			},
		}
		catStatusErr := NewTemplateCatalog(txDB)
		_, err = catStatusErr.SetStatus(ctx, "bill_v2", whatsapp.TemplateStatusApproved)
		if err == nil || err.Error() != "status tx get failed" {
			t.Errorf("expected 'status tx get failed', got %v", err)
		}
	})
}

func TestPickApproved(t *testing.T) {
	t.Run("empty_defs", func(t *testing.T) {
		_, found, err := pickApproved(nil, "en_US")
		if err != nil || found {
			t.Errorf("expected false, nil for empty defs")
		}
	})

	t.Run("none_approved", func(t *testing.T) {
		defs := []whatsapp.TemplateDef{
			{Name: "t1", Locale: "en_US", Status: whatsapp.TemplateStatusPending},
			{Name: "t2", Locale: "en_US", Status: whatsapp.TemplateStatusRejected},
		}
		_, found, err := pickApproved(defs, "en_US")
		if err != nil || found {
			t.Errorf("expected false, nil when none approved")
		}
	})

	t.Run("exact_match", func(t *testing.T) {
		defs := []whatsapp.TemplateDef{
			{Name: "t1", Locale: "en_GB", Status: whatsapp.TemplateStatusApproved},
			{Name: "t2", Locale: "en_US", Status: whatsapp.TemplateStatusApproved},
		}
		def, found, err := pickApproved(defs, "en_US")
		if err != nil || !found || def.Name != "t2" {
			t.Errorf("expected t2, got (%v, %v, %v)", def, found, err)
		}
	})

	t.Run("language_match", func(t *testing.T) {
		defs := []whatsapp.TemplateDef{
			{Name: "t1", Locale: "en_GB", Status: whatsapp.TemplateStatusApproved},
		}
		// Query en_US, match on en_GB because language is en
		def, found, err := pickApproved(defs, "en_US")
		if err != nil || !found || def.Name != "t1" {
			t.Errorf("expected t1, got (%v, %v, %v)", def, found, err)
		}
	})

	t.Run("locale_without_underscore_match", func(t *testing.T) {
		defs := []whatsapp.TemplateDef{
			{Name: "t1", Locale: "en", Status: whatsapp.TemplateStatusApproved},
		}
		def, found, err := pickApproved(defs, "en")
		if err != nil || !found || def.Name != "t1" {
			t.Errorf("expected t1, got (%v, %v, %v)", def, found, err)
		}
	})

	t.Run("first_approved_fallback", func(t *testing.T) {
		defs := []whatsapp.TemplateDef{
			{Name: "t1", Locale: "de_DE", Status: whatsapp.TemplateStatusApproved},
			{Name: "t2", Locale: "es_ES", Status: whatsapp.TemplateStatusApproved},
		}
		// Query fr_FR, should fall back to first approved (de_DE)
		def, found, err := pickApproved(defs, "fr_FR")
		if err != nil || !found || def.Name != "t1" {
			t.Errorf("expected t1, got (%v, %v, %v)", def, found, err)
		}
	})
}
