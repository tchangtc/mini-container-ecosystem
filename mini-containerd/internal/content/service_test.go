package content_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net"
	"testing"

	contentv1 "github.com/containerd/containerd/api/services/content/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/tcherry/mini-container-ecosystem/mini-containerd/internal/content"
)
type testServer struct {
	store    *content.Store
	grpcSrv  *grpc.Server
	socket   string
	listener net.Listener
}

func startTestServer(t *testing.T) *testServer {
	t.Helper()

	tmpDir := t.TempDir()
	store, err := content.NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	socket := tmpDir + "/test.sock"
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	srv := grpc.NewServer()
	contentv1.RegisterContentServer(srv, content.NewService(store))

	go srv.Serve(listener)

	return &testServer{
		store:   store,
		grpcSrv: srv,
		socket:  socket,
		listener: listener,
	}
}

func (ts *testServer) client(t *testing.T) (contentv1.ContentClient, *grpc.ClientConn) {
	t.Helper()
	conn, err := grpc.NewClient("unix://"+ts.socket,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.Dial: %v", err)
	}
	return contentv1.NewContentClient(conn), conn
}

func (ts *testServer) stop() {
	ts.grpcSrv.GracefulStop()
	ts.listener.Close()
}

func TestInfo_NotFound(t *testing.T) {
	ts := startTestServer(t)
	defer ts.stop()

	client, conn := ts.client(t)
	defer conn.Close()

	ctx := context.Background()
	_, err := client.Info(ctx, &contentv1.InfoRequest{Digest: "sha256:deadbeef"})
	if err == nil {
		t.Fatal("expected error for non-existent blob")
	}
}

func TestWriteReadDelete(t *testing.T) {
	ts := startTestServer(t)
	defer ts.stop()

	client, conn := ts.client(t)
	defer conn.Close()
	ctx := context.Background()

	// Test data
	data := []byte("hello mini-containerd content store!")
	expectedDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(data))
	ref := "test-write-1"

	// 1. Write: STAT → WRITE → COMMIT
	writeStream, err := client.Write(ctx)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	// STAT
	if err := writeStream.Send(&contentv1.WriteContentRequest{
		Action: contentv1.WriteAction_STAT,
		Ref:    ref,
	}); err != nil {
		t.Fatalf("STAT send: %v", err)
	}
	statResp, err := writeStream.Recv()
	if err != nil {
		t.Fatalf("STAT recv: %v", err)
	}
	if statResp.Action != contentv1.WriteAction_STAT {
		t.Fatalf("expected STAT response, got %v", statResp.Action)
	}
	if statResp.Offset != 0 {
		t.Fatalf("expected offset 0, got %d", statResp.Offset)
	}

	// WRITE
	if err := writeStream.Send(&contentv1.WriteContentRequest{
		Action: contentv1.WriteAction_WRITE,
		Ref:    ref,
		Offset: 0,
		Data:   data,
	}); err != nil {
		t.Fatalf("WRITE send: %v", err)
	}
	writeResp, err := writeStream.Recv()
	if err != nil {
		t.Fatalf("WRITE recv: %v", err)
	}
	if writeResp.Offset != int64(len(data)) {
		t.Fatalf("expected offset %d, got %d", len(data), writeResp.Offset)
	}

	// COMMIT
	if err := writeStream.Send(&contentv1.WriteContentRequest{
		Action:   contentv1.WriteAction_COMMIT,
		Ref:      ref,
		Offset:   int64(len(data)),
		Data:     data,
		Total:    int64(len(data)),
		Expected: expectedDigest,
	}); err != nil {
		t.Fatalf("COMMIT send: %v", err)
	}
	commitResp, err := writeStream.Recv()
	if err != nil && err != io.EOF {
		t.Fatalf("COMMIT recv: %v", err)
	}
	_ = commitResp
	writeStream.CloseSend()

	// 2. Info
	infoResp, err := client.Info(ctx, &contentv1.InfoRequest{Digest: expectedDigest})
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if infoResp.Info.Digest != expectedDigest {
		t.Fatalf("expected digest %s, got %s", expectedDigest, infoResp.Info.Digest)
	}
	if infoResp.Info.Size != int64(len(data)) {
		t.Fatalf("expected size %d, got %d", len(data), infoResp.Info.Size)
	}

	// 3. Read
	readStream, err := client.Read(ctx, &contentv1.ReadContentRequest{Digest: expectedDigest})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	var readData bytes.Buffer
	for {
		resp, err := readStream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Read recv: %v", err)
		}
		readData.Write(resp.Data)
	}
	if !bytes.Equal(readData.Bytes(), data) {
		t.Fatalf("read data mismatch: got %q, want %q", readData.Bytes(), data)
	}

	// 4. List
	listStream, err := client.List(ctx, &contentv1.ListContentRequest{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	count := 0
	for {
		resp, err := listStream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("List recv: %v", err)
		}
		count += len(resp.Info)
	}
	if count < 1 {
		t.Fatal("expected at least 1 blob in list")
	}

	// 5. Delete
	_, err = client.Delete(ctx, &contentv1.DeleteContentRequest{Digest: expectedDigest})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Verify deleted
	_, err = client.Info(ctx, &contentv1.InfoRequest{Digest: expectedDigest})
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestStatus(t *testing.T) {
	ts := startTestServer(t)
	defer ts.stop()

	client, conn := ts.client(t)
	defer conn.Close()
	ctx := context.Background()

	ref := "test-status-1"

	// Start upload
	writeStream, _ := client.Write(ctx)
	writeStream.Send(&contentv1.WriteContentRequest{
		Action: contentv1.WriteAction_STAT,
		Ref:    ref,
	})
	writeStream.Recv()

	// Check status
	statusResp, err := client.Status(ctx, &contentv1.StatusRequest{Ref: ref})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if statusResp.Status.Ref != ref {
		t.Fatalf("expected ref %s, got %s", ref, statusResp.Status.Ref)
	}

	// Write some data
	writeStream.Send(&contentv1.WriteContentRequest{
		Action: contentv1.WriteAction_WRITE,
		Ref:    ref,
		Offset: 0,
		Data:   []byte("test"),
	})
	resp, _ := writeStream.Recv()
	if resp.Offset != 4 {
		t.Fatalf("expected offset 4, got %d", resp.Offset)
	}

	writeStream.CloseSend()
}

// Ensure we don't have unused import warnings
var _ = emptypb.Empty{}
