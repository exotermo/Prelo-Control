package dev.prelo.bridge.service;

import dev.prelo.bridge.client.PreloCoreClient;
import dev.prelo.bridge.persistence.InboundEventStatus;
import dev.prelo.bridge.persistence.InboundEventStore;
import dev.prelo.bridge.persistence.OutboundReply;
import dev.prelo.bridge.persistence.OutboundReplyStore;
import dev.prelo.bridge.persistence.OwnerContactStore;
import dev.prelo.bridge.web.InboundMessageEvent;
import java.time.Duration;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.HexFormat;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.scheduling.annotation.Async;
import org.springframework.stereotype.Service;

// Orchestrates the whole bridge: an inbound WhatsApp message (already verified, parsed and
// durably recorded by WebhookController) goes to prelo-core as a Task, and whatever the agent
// produces is queued in outbound_replies for OutboundReplyWorker to actually deliver — this
// method never calls messaging-core itself, so a crash after the execution completes cannot lose
// the reply (Fase F point 4). Runs off the request thread (@Async) since the webhook already
// answered 202 before this starts.
@Service
public class InboundMessageHandler {
    private static final Logger log = LoggerFactory.getLogger(InboundMessageHandler.class);
    private static final Duration EXECUTION_TIMEOUT = Duration.ofSeconds(30);
    private static final String OWNER_AGENT_ID = "general";
    private static final String CUSTOMER_AGENT_ID = "customer";

    private final PreloCoreClient preloCoreClient;
    private final AutoReplyGate autoReplyGate;
    private final OwnerContactStore ownerContacts;
    private final InboundEventStore events;
    private final OutboundReplyStore replies;
    private final OutboundMessenger messenger;

    // Fase T: the owner's answer to an approval request — "SIM K7Q2", "não k7q2", "aprovar K7Q2"…
    static final java.util.regex.Pattern APPROVAL_ANSWER = java.util.regex.Pattern.compile(
        "^\\s*(sim|s|aprovar|aprovado|ok|n[aã]o|negar|negado)\\s+([A-Za-z0-9]{4,6})\\s*[.!]?\\s*$", java.util.regex.Pattern.CASE_INSENSITIVE | java.util.regex.Pattern.UNICODE_CASE);

    public InboundMessageHandler(PreloCoreClient preloCoreClient, AutoReplyGate autoReplyGate,
                                  OwnerContactStore ownerContacts, InboundEventStore events, OutboundReplyStore replies,
                                  OutboundMessenger messenger) {
        this.preloCoreClient = preloCoreClient;
        this.autoReplyGate = autoReplyGate;
        this.ownerContacts = ownerContacts;
        this.events = events;
        this.replies = replies;
        this.messenger = messenger;
    }

    @Async
    public void handleAsync(InboundMessageEvent event) {
        handle(event);
    }

    void handle(InboundMessageEvent event) {
        if (!autoReplyGate.isEnabled()) {
            log.warn("auto_reply_paused messageId={} channelIdentityId={} before=prelo", event.messageId(), event.channelIdentityId());
            events.markStatus(event.messageId(), InboundEventStatus.FAILED);
            return;
        }
        events.markStatus(event.messageId(), InboundEventStatus.PROCESSING);

        // Sender→authorization mapping (Fase F point 4): a contact that is not a known owner
        // number never gets the "general" agent's capabilities (delegate_to_agent, tools) —
        // it gets "customer", which the catalog defines with none. Fase T: compared by
        // normalized number, so "5541…@s.whatsapp.net" matches the stored "+5541…".
        boolean owner = ownerContacts.isOwner(event.from());
        if (owner && handleApprovalAnswer(event)) return;
        String agentId = owner ? OWNER_AGENT_ID : CUSTOMER_AGENT_ID;
        PreloCoreClient.ExecutionResult result = preloCoreClient.run(event.text(), agentId, event.from(), EXECUTION_TIMEOUT);

        if (!"COMPLETED".equals(result.status()) || result.result() == null || result.result().isBlank()) {
            log.warn("no reply sent for message {}: prelo-core execution ended as {} ({})",
                event.messageId(), result.status(), result.error());
            events.markStatus(event.messageId(), InboundEventStatus.FAILED);
            return;
        }

        replies.insert(OutboundReply.pending(event.messageId(), event.channelIdentityId(), event.from(), result.result()));
        events.markStatus(event.messageId(), InboundEventStatus.DONE);
        log.info("reply_queued contactHash={} channelIdentityId={} taskId={} agentId={} responseHash={}",
            shortHash(event.from()), event.channelIdentityId(), result.taskId(), agentId, shortHash(result.result()));
    }

    /**
     * An owner's "SIM K7Q2"/"NÃO K7Q2" decides that approval in prelo-core and gets a short
     * confirmation — it never becomes a task. Only called for owner contacts: anyone else typing
     * the same words is just a normal message.
     */
    boolean handleApprovalAnswer(InboundMessageEvent event) {
        java.util.regex.Matcher m = APPROVAL_ANSWER.matcher(event.text() == null ? "" : event.text());
        if (!m.matches()) return false;
        String word = m.group(1).toLowerCase(java.util.Locale.ROOT);
        boolean approve = !(word.startsWith("n"));
        String code = m.group(2).toUpperCase(java.util.Locale.ROOT);
        String by = "whatsapp:" + dev.prelo.bridge.contact.ContactAddress.canonical(event.from());
        PreloCoreClient.DecisionOutcome outcome = preloCoreClient.decideApproval(code, approve, by);
        String reply = switch (outcome) {
            case APPROVED -> "✅ Autorizado (" + code + "). O agente vai continuar.";
            case DENIED -> "❌ Negado (" + code + "). A ação não será executada.";
            case NOT_FOUND -> "Não encontrei o pedido " + code + ". Confira o código.";
            case ALREADY_CLOSED -> "O pedido " + code + " já tinha sido decidido ou expirou.";
            case FAILED -> "Não consegui registrar sua resposta para " + code + " agora. Tente de novo ou use o painel.";
        };
        messenger.reply(event.channelIdentityId(), event.from(), reply);
        events.markStatus(event.messageId(), InboundEventStatus.DONE);
        log.info("approval_answer contactHash={} code={} outcome={}", shortHash(event.from()), code, outcome);
        return true;
    }

    private static String shortHash(String value) {
        try {
            byte[] digest = MessageDigest.getInstance("SHA-256").digest(value.getBytes(StandardCharsets.UTF_8));
            return HexFormat.of().formatHex(digest, 0, 8);
        } catch (Exception exception) {
            return "unavailable";
        }
    }
}
