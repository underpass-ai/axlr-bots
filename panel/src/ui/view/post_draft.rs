//! What a bot is about to publish, drawn as a post before a person says
//! yes: who writes, the words with their links, what goes with them, how
//! long it is. Any channel: the panel draws the draft, not the network.

use iced::widget::text::{LineHeight, Span, Wrapping};
use iced::widget::{Space, column, container, rich_text, row, span, text};
use iced::{Alignment, Border, Length, never};

use super::widgets::{BODY, El, REGULAR, SEMIBOLD, SMALL, bold, chip, copy_button, icon, muted, section_title};
use crate::domain::PostDraft;
use crate::ui::styles::{self, RADIUS, RADIUS_S};
use crate::ui::{Icon, Tokens};

/// The post card: author, text, attachments, then the count. `author` is
/// the bot's name; it writes the post, the person only lets it through.
pub fn view<'a>(draft: PostDraft, author: String, tokens: Tokens) -> El<'a> {
    let initial = author
        .chars()
        .next()
        .map(|c| c.to_uppercase().to_string())
        .unwrap_or_default();
    let avatar = container(bold(initial).size(BODY).color(tokens.on_accent))
        .width(40)
        .height(40)
        .center_x(40)
        .center_y(40)
        .style(move |_| container::Style {
            background: Some(tokens.accent.into()),
            border: Border {
                radius: 20.0.into(),
                ..Default::default()
            },
            ..Default::default()
        });
    let header = row![
        bold(author.clone()).size(BODY),
        muted(format!("· {} · not posted yet", draft.channel), tokens),
    ]
    .spacing(6)
    .align_y(Alignment::Center);

    let mut body = column![header, words(&draft, tokens)].spacing(6).width(Length::Fill);
    if !draft.attachments.is_empty() {
        body = body.push(Space::new().height(4)).push(attachments(&draft, tokens));
    }

    let links = draft.links().len();
    let count = format!(
        "{} characters · {} link{}{}",
        draft.characters(),
        links,
        if links == 1 { "" } else { "s" },
        if draft.attachments.is_empty() {
            String::new()
        } else {
            format!(
                " · {} attachment{}",
                draft.attachments.len(),
                if draft.attachments.len() == 1 { "" } else { "s" }
            )
        }
    );

    container(
        column![
            row![
                section_title(Icon::File, "As it would read", tokens),
                Space::new().width(Length::Fill),
                copy_button("Copy text", draft.text.clone(), tokens),
            ]
            .align_y(Alignment::Center),
            row![avatar, body].spacing(12).align_y(Alignment::Start),
            row![icon(Icon::Dot, 12.0, tokens.muted), muted(count, tokens)]
                .spacing(6)
                .align_y(Alignment::Center),
        ]
        .spacing(12),
    )
    .padding(16)
    .width(Length::Fill)
    .style(move |theme| container::Style {
        border: Border {
            radius: RADIUS.into(),
            ..styles::card(tokens)(theme).border
        },
        ..styles::card(tokens)(theme)
    })
    .into()
}

/// The text as a reader sees it: paragraphs kept, links in the accent.
fn words<'a>(draft: &PostDraft, tokens: Tokens) -> El<'a> {
    let mut spans: Vec<Span<'a, iced::Never, iced::Font>> = Vec::new();
    for (p, paragraph) in draft.text.split('\n').enumerate() {
        if p > 0 {
            spans.push(span("\n"));
        }
        for (w, word) in paragraph.split(' ').enumerate() {
            if w > 0 {
                spans.push(span(" "));
            }
            if word.starts_with("https://") || word.starts_with("http://") {
                spans.push(span(word.to_string()).color(tokens.accent).font(SEMIBOLD));
            } else if word.len() > 1 && (word.starts_with('@') || word.starts_with('#')) {
                spans.push(span(word.to_string()).color(tokens.accent));
            } else {
                spans.push(span(word.to_string()));
            }
        }
    }
    rich_text(spans)
        .on_link_click(never)
        .font(REGULAR)
        .size(BODY)
        .line_height(LineHeight::Relative(1.4))
        .wrapping(Wrapping::WordOrGlyph)
        .width(Length::Fill)
        .color(tokens.text)
        .into()
}

/// One dashed box per attachment: the picture is drawn on the node, the
/// panel shows what it is.
fn attachments<'a>(draft: &PostDraft, tokens: Tokens) -> El<'a> {
    row(draft.attachments.clone().into_iter().enumerate().map(|(i, line)| {
        container(
            column![
                row![icon(Icon::File, 14.0, tokens.muted), chip(format!("{}", i + 1), tokens)]
                    .spacing(6)
                    .align_y(Alignment::Center),
                text(line)
                    .size(SMALL)
                    .color(tokens.text)
                    .wrapping(Wrapping::WordOrGlyph),
            ]
            .spacing(8),
        )
        .padding(10)
        .width(Length::FillPortion(1))
        .style(move |_| container::Style {
            background: Some(tokens.raised.into()),
            border: Border {
                color: tokens.border,
                width: 1.0,
                radius: RADIUS_S.into(),
            },
            ..Default::default()
        })
        .into()
    }))
    .spacing(8)
    .width(Length::Fill)
    .into()
}
