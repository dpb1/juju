// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package storage_test

import (
	"io/ioutil"
	"path/filepath"

	"github.com/juju/names"
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"
	"gopkg.in/juju/charm.v4/hooks"

	"github.com/juju/juju/api/watcher"
	"github.com/juju/juju/apiserver/params"
	corestorage "github.com/juju/juju/storage"
	"github.com/juju/juju/testing"
	"github.com/juju/juju/worker/uniter/hook"
	"github.com/juju/juju/worker/uniter/storage"
)

type attachmentsSuite struct {
	testing.BaseSuite
}

var _ = gc.Suite(&attachmentsSuite{})

func (s *attachmentsSuite) TestNewAttachments(c *gc.C) {
	stateDir := filepath.Join(c.MkDir(), "nonexistent")
	unitTag := names.NewUnitTag("mysql/0")
	abort := make(chan struct{})
	st := &mockStorageAccessor{
		unitStorageAttachments: func(u names.UnitTag) ([]params.StorageAttachment, error) {
			c.Assert(u, gc.Equals, unitTag)
			return nil, nil
		},
	}

	att, err := storage.NewAttachments(st, unitTag, stateDir, abort)
	c.Assert(err, jc.ErrorIsNil)
	defer func() {
		err := att.Stop()
		c.Assert(err, jc.ErrorIsNil)
	}()
	// state dir should have been created.
	c.Assert(stateDir, jc.IsDirectory)
}

func (s *attachmentsSuite) TestNewAttachmentsInit(c *gc.C) {
	stateDir := c.MkDir()
	unitTag := names.NewUnitTag("mysql/0")
	abort := make(chan struct{})

	// Simulate remote state returning a single Alive storage attachment.
	attachments := []params.StorageAttachment{{
		StorageTag: "storage-data-0",
		Life:       params.Alive,
	}}
	st := &mockStorageAccessor{
		unitStorageAttachments: func(u names.UnitTag) ([]params.StorageAttachment, error) {
			c.Assert(u, gc.Equals, unitTag)
			return attachments, nil
		},
		watchStorageAttachment: func(s names.StorageTag, u names.UnitTag) (watcher.NotifyWatcher, error) {
			return newMockNotifyWatcher(), nil
		},
	}

	storageTag := names.NewStorageTag("data/0")
	withAttachments := func(f func(*storage.Attachments)) {
		att, err := storage.NewAttachments(st, unitTag, stateDir, abort)
		c.Assert(err, jc.ErrorIsNil)
		defer func() {
			err := att.Stop()
			c.Assert(err, jc.ErrorIsNil)
		}()
		f(att)
	}

	// No state files, so no storagers will be started.
	var called int
	withAttachments(func(att *storage.Attachments) {
		called++
		err := att.ValidateHook(hook.Info{
			Kind:      hooks.StorageAttached,
			StorageId: storageTag.Id(),
		})
		c.Assert(err, gc.ErrorMatches, `unknown storage "data/0"`)
	})
	c.Assert(called, gc.Equals, 1)

	// Commit a storage-attached to local state and try again.
	state0, err := storage.ReadStateFile(stateDir, storageTag)
	c.Assert(err, jc.ErrorIsNil)
	err = state0.CommitHook(hook.Info{Kind: hooks.StorageAttached, StorageId: "data/0"})
	c.Assert(err, jc.ErrorIsNil)
	// Create an extra one so we can make sure it gets removed.
	state1, err := storage.ReadStateFile(stateDir, names.NewStorageTag("data/1"))
	c.Assert(err, jc.ErrorIsNil)
	err = state1.CommitHook(hook.Info{Kind: hooks.StorageAttached, StorageId: "data/1"})
	c.Assert(err, jc.ErrorIsNil)

	withAttachments(func(att *storage.Attachments) {
		called++
		err := att.ValidateHook(hook.Info{
			Kind:      hooks.StorageDetached,
			StorageId: storageTag.Id(),
		})
		c.Assert(err, jc.ErrorIsNil)
		err = att.ValidateHook(hook.Info{
			Kind:      hooks.StorageAttached,
			StorageId: "data/1",
		})
		c.Assert(err, gc.ErrorMatches, `unknown storage "data/1"`)
	})
	c.Assert(called, gc.Equals, 2)
	c.Assert(filepath.Join(stateDir, "data-0"), jc.IsNonEmptyFile)
	c.Assert(filepath.Join(stateDir, "data-1"), jc.DoesNotExist)
}

func (s *attachmentsSuite) TestAttachmentsUpdateShortCircuitDeath(c *gc.C) {
	stateDir := c.MkDir()
	unitTag := names.NewUnitTag("mysql/0")
	abort := make(chan struct{})

	var ensuredDead, removed bool
	storageTag := names.NewStorageTag("data/0")
	attachment := params.StorageAttachment{
		StorageTag: storageTag.String(),
		UnitTag:    unitTag.String(),
		Life:       params.Dying,
	}
	st := &mockStorageAccessor{
		unitStorageAttachments: func(u names.UnitTag) ([]params.StorageAttachment, error) {
			c.Assert(u, gc.Equals, unitTag)
			return nil, nil
		},
		watchStorageAttachment: func(s names.StorageTag, u names.UnitTag) (watcher.NotifyWatcher, error) {
			w := newMockNotifyWatcher()
			return w, nil
		},
		storageAttachment: func(s names.StorageTag, u names.UnitTag) (params.StorageAttachment, error) {
			return attachment, nil
		},
		ensureDead: func(s names.StorageTag, u names.UnitTag) error {
			ensuredDead = true
			c.Assert(s, gc.Equals, storageTag)
			c.Assert(u, gc.Equals, unitTag)
			return nil
		},
		remove: func(s names.StorageTag, u names.UnitTag) error {
			removed = true
			c.Assert(s, gc.Equals, storageTag)
			c.Assert(u, gc.Equals, unitTag)
			return nil
		},
	}

	att, err := storage.NewAttachments(st, unitTag, stateDir, abort)
	c.Assert(err, jc.ErrorIsNil)
	defer func() {
		err := att.Stop()
		c.Assert(err, jc.ErrorIsNil)
	}()
	err = att.UpdateStorage([]names.StorageTag{storageTag})
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(ensuredDead, jc.IsTrue)
	c.Assert(removed, jc.IsTrue)
}

func (s *attachmentsSuite) TestAttachmentsStorage(c *gc.C) {
	stateDir := c.MkDir()
	unitTag := names.NewUnitTag("mysql/0")
	abort := make(chan struct{})

	storageTag := names.NewStorageTag("data/0")
	attachment := params.StorageAttachment{
		StorageTag: storageTag.String(),
		UnitTag:    unitTag.String(),
		Life:       params.Alive,
		Kind:       params.StorageKindBlock,
		Location:   "/dev/sdb",
	}

	st := &mockStorageAccessor{
		unitStorageAttachments: func(u names.UnitTag) ([]params.StorageAttachment, error) {
			c.Assert(u, gc.Equals, unitTag)
			return nil, nil
		},
		watchStorageAttachment: func(s names.StorageTag, u names.UnitTag) (watcher.NotifyWatcher, error) {
			w := newMockNotifyWatcher()
			w.changes <- struct{}{}
			return w, nil
		},
		storageAttachment: func(s names.StorageTag, u names.UnitTag) (params.StorageAttachment, error) {
			c.Assert(s, gc.Equals, storageTag)
			return attachment, nil
		},
	}

	att, err := storage.NewAttachments(st, unitTag, stateDir, abort)
	c.Assert(err, jc.ErrorIsNil)
	defer func() {
		err := att.Stop()
		c.Assert(err, jc.ErrorIsNil)
	}()

	// There should be no context for data/0 until a hook is queued.
	_, ok := att.Storage(storageTag)
	c.Assert(ok, jc.IsFalse)

	err = att.UpdateStorage([]names.StorageTag{storageTag})
	c.Assert(err, jc.ErrorIsNil)
	hi := waitOneHook(c, att.Hooks())
	c.Assert(hi, gc.Equals, hook.Info{
		Kind:      hooks.StorageAttached,
		StorageId: storageTag.Id(),
	})

	ctx, ok := att.Storage(storageTag)
	c.Assert(ok, jc.IsTrue)
	c.Assert(ctx, gc.NotNil)
	c.Assert(ctx.Tag(), gc.Equals, storageTag)
	c.Assert(ctx.Kind(), gc.Equals, corestorage.StorageKindBlock)
	c.Assert(ctx.Location(), gc.Equals, "/dev/sdb")
}

func (s *attachmentsSuite) TestAttachmentsCommitHook(c *gc.C) {
	stateDir := c.MkDir()
	unitTag := names.NewUnitTag("mysql/0")
	abort := make(chan struct{})

	var ensuredDead bool
	storageTag := names.NewStorageTag("data/0")
	attachment := params.StorageAttachment{
		StorageTag: storageTag.String(),
		UnitTag:    unitTag.String(),
		Life:       params.Alive,
		Kind:       params.StorageKindBlock,
		Location:   "/dev/sdb",
	}
	st := &mockStorageAccessor{
		unitStorageAttachments: func(u names.UnitTag) ([]params.StorageAttachment, error) {
			c.Assert(u, gc.Equals, unitTag)
			return nil, nil
		},
		watchStorageAttachment: func(s names.StorageTag, u names.UnitTag) (watcher.NotifyWatcher, error) {
			w := newMockNotifyWatcher()
			w.changes <- struct{}{}
			return w, nil
		},
		storageAttachment: func(s names.StorageTag, u names.UnitTag) (params.StorageAttachment, error) {
			c.Assert(s, gc.Equals, storageTag)
			return attachment, nil
		},
		ensureDead: func(s names.StorageTag, u names.UnitTag) error {
			ensuredDead = true
			c.Assert(s, gc.Equals, storageTag)
			return nil
		},
	}

	att, err := storage.NewAttachments(st, unitTag, stateDir, abort)
	c.Assert(err, jc.ErrorIsNil)
	defer func() {
		err := att.Stop()
		c.Assert(err, jc.ErrorIsNil)
	}()
	err = att.UpdateStorage([]names.StorageTag{storageTag})
	c.Assert(err, jc.ErrorIsNil)

	stateFile := filepath.Join(stateDir, "data-0")
	c.Assert(stateFile, jc.DoesNotExist)

	err = att.CommitHook(hook.Info{
		Kind:      hooks.StorageAttached,
		StorageId: storageTag.Id(),
	})
	c.Assert(err, jc.ErrorIsNil)
	data, err := ioutil.ReadFile(stateFile)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(string(data), gc.Equals, "attached: true\n")

	c.Assert(ensuredDead, jc.IsFalse)
	err = att.CommitHook(hook.Info{
		Kind:      hooks.StorageDetached,
		StorageId: storageTag.Id(),
	})
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(stateFile, jc.DoesNotExist)
	c.Assert(ensuredDead, jc.IsTrue)
}

type attachmentsUpdateSuite struct {
	testing.BaseSuite
	unitTag          names.UnitTag
	storageTag0      names.StorageTag
	storageTag1      names.StorageTag
	attachmentsByTag map[names.StorageTag]*params.StorageAttachment
	att              *storage.Attachments
}

var _ = gc.Suite(&attachmentsUpdateSuite{})

func (s *attachmentsUpdateSuite) SetUpTest(c *gc.C) {
	s.unitTag = names.NewUnitTag("mysql/0")
	s.storageTag0 = names.NewStorageTag("data/0")
	s.storageTag1 = names.NewStorageTag("data/1")
	s.attachmentsByTag = map[names.StorageTag]*params.StorageAttachment{
		s.storageTag0: {
			StorageTag: s.storageTag0.String(),
			UnitTag:    s.unitTag.String(),
			Life:       params.Alive,
			Kind:       params.StorageKindBlock,
			Location:   "/dev/sdb",
		},
		s.storageTag1: {
			StorageTag: s.storageTag1.String(),
			UnitTag:    s.unitTag.String(),
			Life:       params.Dying,
			Kind:       params.StorageKindBlock,
			Location:   "/dev/sdb",
		},
	}

	st := &mockStorageAccessor{
		unitStorageAttachments: func(u names.UnitTag) ([]params.StorageAttachment, error) {
			c.Assert(u, gc.Equals, s.unitTag)
			return nil, nil
		},
		watchStorageAttachment: func(storageTag names.StorageTag, u names.UnitTag) (watcher.NotifyWatcher, error) {
			w := newMockNotifyWatcher()
			w.changes <- struct{}{}
			return w, nil
		},
		storageAttachment: func(storageTag names.StorageTag, u names.UnitTag) (params.StorageAttachment, error) {
			att, ok := s.attachmentsByTag[storageTag]
			c.Assert(ok, jc.IsTrue)
			return *att, nil
		},
		ensureDead: func(storageTag names.StorageTag, u names.UnitTag) error {
			c.Assert(storageTag, gc.Equals, s.storageTag1)
			return nil
		},
		remove: func(storageTag names.StorageTag, u names.UnitTag) error {
			c.Assert(storageTag, gc.Equals, s.storageTag1)
			return nil
		},
	}

	stateDir := c.MkDir()
	abort := make(chan struct{})
	var err error
	s.att, err = storage.NewAttachments(st, s.unitTag, stateDir, abort)
	c.Assert(err, jc.ErrorIsNil)
	s.AddCleanup(func(c *gc.C) {
		err := s.att.Stop()
		c.Assert(err, jc.ErrorIsNil)
	})
}

func (s *attachmentsUpdateSuite) TestAttachmentsUpdateUntrackedAlive(c *gc.C) {
	// data/0 is initially unattached and untracked, so
	// updating with Alive will cause a storager to be
	// started and a storage-attached event to be emitted.
	for i := 0; i < 2; i++ {
		// Updating twice, to ensure idempotency.
		err := s.att.UpdateStorage([]names.StorageTag{s.storageTag0})
		c.Assert(err, jc.ErrorIsNil)
	}
	hi := waitOneHook(c, s.att.Hooks())
	c.Assert(hi, gc.Equals, hook.Info{
		Kind:      hooks.StorageAttached,
		StorageId: s.storageTag0.Id(),
	})
}

func (s *attachmentsUpdateSuite) TestAttachmentsUpdateUntrackedDying(c *gc.C) {
	// data/1 is initially unattached and untracked, so
	// updating with Dying will not cause a storager to
	// be started.
	err := s.att.UpdateStorage([]names.StorageTag{s.storageTag1})
	c.Assert(err, jc.ErrorIsNil)
	assertNoHooks(c, s.att.Hooks())
}

func (s *attachmentsUpdateSuite) TestAttachmentsUpdateShortCircuitNoHooks(c *gc.C) {
	// Cause an Alive hook to be queued, but don't consume it;
	// then update to Dying, and ensure no hooks are generated.
	// Additionally, the storager should be stopped and no
	// longer tracked.
	s.attachmentsByTag[s.storageTag1].Life = params.Alive
	err := s.att.UpdateStorage([]names.StorageTag{s.storageTag1})
	c.Assert(err, jc.ErrorIsNil)
	err = s.att.ValidateHook(hook.Info{
		Kind:      hooks.StorageAttached,
		StorageId: s.storageTag1.Id(),
	})
	c.Assert(err, jc.ErrorIsNil)

	s.attachmentsByTag[s.storageTag1].Life = params.Dying
	err = s.att.UpdateStorage([]names.StorageTag{s.storageTag1})
	c.Assert(err, jc.ErrorIsNil)
	assertNoHooks(c, s.att.Hooks())
	err = s.att.ValidateHook(hook.Info{
		Kind:      hooks.StorageAttached,
		StorageId: s.storageTag1.Id(),
	})
	c.Assert(err, gc.ErrorMatches, `unknown storage "data/1"`)
}
