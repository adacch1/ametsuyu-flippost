// One-time slash-command registration for the /modem command (guild-scoped, so
// it appears instantly). Re-run whenever you add a subcommand.
//   docker compose run --rm bot node register.js
import { REST, Routes, SlashCommandBuilder } from 'discord.js';

const { DISCORD_TOKEN, DISCORD_APP_ID, DISCORD_GUILD_ID } = process.env;
if (!DISCORD_TOKEN || !DISCORD_APP_ID || !DISCORD_GUILD_ID) {
  console.error('Set DISCORD_TOKEN, DISCORD_APP_ID, DISCORD_GUILD_ID in .env');
  process.exit(1);
}

const subs = ['status', 'thermal', 'battery', 'signal', 'clients', 'bands', 'usage', 'network', 'prefer5g', 'tether', 'cooldown', 'sms'];

const cmd = new SlashCommandBuilder().setName('modem').setDescription('Z Flip 5 modem control');
for (const s of subs) cmd.addSubcommand((sc) => sc.setName(s).setDescription(`modem ${s}`));

const rest = new REST({ version: '10' }).setToken(DISCORD_TOKEN);
await rest.put(Routes.applicationGuildCommands(DISCORD_APP_ID, DISCORD_GUILD_ID), { body: [cmd.toJSON()] });
console.log('registered /modem with subcommands:', subs.join(', '));
