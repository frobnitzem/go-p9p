package p9p

import "context"

// Handler defines an interface for 9p message handlers. A handler
// implementation could be used to intercept calls of all types before sending
// them to the next handler.
// This is different than roundTripper because it handles multiple messages,
// and needs to provide a shutdown callback.
type Handler interface {
	Handle(ctx context.Context, msg Message) (Message, error)
	Stop(error) error
}

type sessionHandler struct {
	s     Session
	msize int
	// TODO(frobnitzem): make use of version to audit messages at this level
}

// SSession returns a handler that transforms messages
// into function calls (dispatching to Session's methods).
// Since the ServeConn function calls the handler from
// gorotines, no concurrency is managed by the handler this
// defined by SSession.
//
// Instead, the Handler simply turns messages into function
// calls on the session.
func SSession(session Session) Handler {
	msize, _ := session.Version()
	return sessionHandler{session, msize}
}

func (sess sessionHandler) Stop(err error) error {
	return sess.s.Stop(err)
}

func (sess sessionHandler) Handle(ctx context.Context,
	msg Message) (Message, error) {
	session := sess.s
	msize := sess.msize
	switch msg := msg.(type) {
	case MessageTauth:
		qid, err := session.Auth(ctx, msg.Afid, msg.Uname, msg.Aname)
		if err != nil {
			return nil, err
		}

		return MessageRauth{Qid: qid}, nil
	case MessageTattach:
		qid, err := session.Attach(ctx, msg.Fid, msg.Afid, msg.Uname, msg.Aname)
		if err != nil {
			return nil, err
		}

		return MessageRattach{
			Qid: qid,
		}, nil
	case MessageTwalk:
		// TODO(stevvooe): This is one of the places where we need to manage
		// fid allocation lifecycle. We need to reserve the fid, then, if this
		// call succeeds, we should alloc the fid for future uses. Also need
		// to interact correctly with concurrent clunk and the flush of this
		// walk message.
		qids, err := session.Walk(ctx, msg.Fid, msg.Newfid, msg.Wnames...)
		if err != nil {
			return nil, err
		}

		return MessageRwalk{
			Qids: qids,
		}, nil
	case MessageTopen:
		qid, iounit, err := session.Open(ctx, msg.Fid, msg.Mode)
		if err != nil {
			return nil, err
		}

		return MessageRopen{
			Qid:    qid,
			IOUnit: iounit,
		}, nil
	case MessageTcreate:
		qid, iounit, err := session.Create(ctx, msg.Fid, msg.Name, msg.Perm, msg.Mode)
		if err != nil {
			return nil, err
		}

		return MessageRcreate{
			Qid:    qid,
			IOUnit: iounit,
		}, nil
	case MessageTread:
		// Re-write incoming Treads so that handler
		// can always respond with a message of the correct msize.
		// TODO(frobnitzem): Re-use these read buffers?
		count := int(msg.Count)
		if count > msize-11 {
			count = msize - 11 // TODO: enforce larger msize in negotiation
			if count < 0 {
				count = 0
			}
		}
		p := make([]byte, count)
		n, err := session.Read(ctx, msg.Fid, p, int64(msg.Offset))
		if err != nil {
			return nil, err
		}

		return MessageRread{
			Data: p[:n],
		}, nil
	case MessageTwrite:
		n, err := session.Write(ctx, msg.Fid, msg.Data, int64(msg.Offset))
		if err != nil {
			return nil, err
		}

		return MessageRwrite{
			Count: uint32(n),
		}, nil
	case MessageTclunk:
		if err := session.Clunk(ctx, msg.Fid); err != nil {
			return nil, err
		}

		return MessageRclunk{}, nil
	case MessageTremove:
		if err := session.Remove(ctx, msg.Fid); err != nil {
			return nil, err
		}

		return MessageRremove{}, nil
	case MessageTstat:
		dir, err := session.Stat(ctx, msg.Fid)
		if err != nil {
			return nil, err
		}

		return MessageRstat{
			Stat: dir,
		}, nil
	case MessageTwstat:
		if err := session.WStat(ctx, msg.Fid, msg.Stat); err != nil {
			return nil, err
		}

		return MessageRwstat{}, nil
	default:
		return nil, ErrUnknownMsg
	}
}
