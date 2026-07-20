package whatsappdalgo

import (
	"context"
	"testing"
	"time"

	"github.com/bots-go-framework/bots-fw-whatsapp/whatsapp"
	"github.com/dal-go/dalgo/adapters/dalgo2memory"
	"github.com/dal-go/dalgo/dal"
)

func TestStores_RetainWhatsAppKeysAndBehavior(t *testing.T) {
	ctx := context.Background()
	db := dalgo2memory.NewDB()

	subjects := NewSubjectStore(db)
	if err := subjects.PutSubject(ctx, "bot-1", "wamid-1", "invoice", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("PutSubject() error: %v", err)
	}
	if subject, found, err := subjects.GetSubject(ctx, "bot-1", "wamid-1"); err != nil || !found || subject != "invoice" {
		t.Fatalf("GetSubject() = (%q, %v, %v)", subject, found, err)
	}

	chats := NewChatDataStore(db)
	if err := chats.SaveChatData(ctx, "bot-1", "chat-1", &whatsapp.WaChatData{RecentInboundIDs: []string{"wamid-1"}}); err != nil {
		t.Fatalf("SaveChatData() error: %v", err)
	}
	chat, err := chats.GetChatData(ctx, "bot-1", "chat-1")
	if err != nil || len(chat.RecentInboundIDs) != 1 {
		t.Fatalf("GetChatData() = (%#v, %v)", chat, err)
	}

	catalog := NewTemplateCatalog(db)
	if err := catalog.Upsert(ctx, "invoice", whatsapp.TemplateDef{Name: "invoice_en", Locale: "en", Status: whatsapp.TemplateStatusApproved}); err != nil {
		t.Fatalf("Upsert() error: %v", err)
	}
	if def, found, err := catalog.Get(ctx, "invoice", "en_US"); err != nil || !found || def.Name != "invoice_en" {
		t.Fatalf("Get() = (%#v, %v, %v)", def, found, err)
	}
}

func TestStores_ResolveDatabasePerOperation(t *testing.T) {
	db := dalgo2memory.NewDB()
	calls := 0
	store := NewSubjectStoreWithProvider(func(context.Context) (dal.DB, error) {
		calls++
		return db, nil
	})
	ctx := context.Background()
	if err := store.PutSubject(ctx, "bot", "wamid", "subject", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("PutSubject() error: %v", err)
	}
	if _, found, err := store.GetSubject(ctx, "bot", "wamid"); err != nil || !found {
		t.Fatalf("GetSubject() = found:%v error:%v", found, err)
	}
	if calls != 2 {
		t.Fatalf("DB provider calls = %d, want 2", calls)
	}
}
