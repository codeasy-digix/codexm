// Compact bridge contract; checked against nativecli.Methods at web bootstrap.
export const bridgeContract = {
  protocolVersion: 1,
  methods: [
    { name: "capabilities", mutation: false, model: false },
    { name: "snapshot", mutation: false, model: false },
    { name: "accounts.refresh", mutation: false, model: false },
    { name: "accounts.remove", mutation: true, model: false },
    { name: "accounts.setDefault", mutation: true, model: false },
    { name: "login.start", mutation: true, model: false },
    { name: "login.status", mutation: false, model: false },
    { name: "login.cancel", mutation: true, model: false },
    { name: "chats.archive", mutation: true, model: false },
    { name: "chats.unarchive", mutation: true, model: false },
    { name: "terminal.open", mutation: true, model: false },
    { name: "terminal.read", mutation: false, model: false },
    { name: "terminal.input", mutation: true, model: false },
    { name: "terminal.close", mutation: true, model: false },
  ],
};
