// Compact bridge contract; checked against nativecli.Methods at web bootstrap.
export const bridgeContract = {
  protocolVersion: 1,
  methods: [
    { name: "capabilities", mutation: false, model: false },
    { name: "snapshot", mutation: false, model: false },
    { name: "accounts.refresh", mutation: false, model: false },
    { name: "terminal.open", mutation: true, model: false },
    { name: "terminal.read", mutation: false, model: false },
    { name: "terminal.input", mutation: true, model: false },
    { name: "terminal.close", mutation: true, model: false },
  ],
};
