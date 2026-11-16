//! The organic hover in a window of its own: a card, a list that scrolls
//! and a few tiles, wired the way the panel wires them. Move the pointer
//! over them; the buttons switch the theme and turn the motion off.
//!
//! `cargo run --example organic` (`-- dark` for the dark theme;
//! `ICED_BACKEND=tiny-skia` for the software renderer).

use std::borrow::Cow;
use std::time::Instant;

use axlr_bots_panel::ui::organic::{self, Hover, Motion, Signal, Surface, Tint};
use axlr_bots_panel::ui::styles::{self, RADIUS, RADIUS_S};
use axlr_bots_panel::ui::view::widgets::{REGULAR, SEMIBOLD};
use axlr_bots_panel::ui::{Tokens, Tone};
use iced::widget::{Space, button, column, container, row, scrollable, text};
use iced::{Alignment, Background, Border, Element, Length, Padding, Size, Subscription, Theme};

const BOTS: [&str; 12] = [
    "researcher",
    "reviewer",
    "builder",
    "watcher",
    "planner",
    "writer",
    "tester",
    "courier",
    "archivist",
    "scout",
    "editor",
    "keeper",
];

/// The demo's state: what the panel's `App` keeps for the organic hover,
/// and the theme.
struct Demo {
    hover: Hover,
    tokens: Tokens,
}

#[derive(Debug, Clone)]
enum Message {
    /// A surface tells about the pointer.
    Hover(Signal),
    /// A frame, while a glow is changing.
    Frame(Instant),
    SwitchTheme,
    SwitchMotion,
}

fn main() -> iced::Result {
    let dark = std::env::args().any(|argument| argument == "dark");
    iced::application(
        move || Demo {
            hover: Hover::new(Motion::On),
            tokens: if dark { Tokens::DARK } else { Tokens::LIGHT },
        },
        update,
        view,
    )
    .title("Organic hover")
    .theme(|demo: &Demo| demo.tokens.theme())
    .subscription(subscription)
    .window_size(Size::new(960.0, 560.0))
    .settings(iced::Settings {
        fonts: vec![
            Cow::Borrowed(include_bytes!("../assets/fonts/Lexend-Regular.ttf").as_slice()),
            Cow::Borrowed(include_bytes!("../assets/fonts/Lexend-SemiBold.ttf").as_slice()),
        ],
        default_font: REGULAR,
        ..iced::Settings::default()
    })
    .run()
}

fn update(demo: &mut Demo, message: Message) {
    match message {
        Message::Hover(signal) => demo.hover.signal(signal),
        Message::Frame(now) => demo.hover.tick(now),
        Message::SwitchTheme => {
            demo.tokens = if demo.tokens.dark { Tokens::LIGHT } else { Tokens::DARK };
        }
        Message::SwitchMotion => {
            let motion = demo.hover.motion().toggled();
            demo.hover.set_motion(motion);
        }
    }
}

/// Frames only while a glow is changing.
fn subscription(demo: &Demo) -> Subscription<Message> {
    organic::frames(&demo.hover).map(Message::Frame)
}

fn view(demo: &Demo) -> Element<'_, Message> {
    let (hover, tokens) = (&demo.hover, demo.tokens);
    let tint = Tint::of(tokens);
    let muted = |words: &'static str| text(words).size(13).color(tokens.muted);
    let strong = |words: &'static str| text(words).size(14).color(tokens.text).font(SEMIBOLD);
    let pill = |tone: Tone, words: &'static str| {
        container(
            text(words)
                .size(13)
                .font(SEMIBOLD)
                .color(styles::pill_ink(tokens, tone)),
        )
        .padding(Padding::from([2, 8]))
        .style(styles::pill(tokens, tone))
    };

    // A card: the backdrop goes inside the box that paints the card, and
    // the card's padding inside the backdrop.
    let card = container(
        organic::backdrop(
            hover,
            Surface::of("card"),
            tint,
            container(
                column![
                    strong("builder"),
                    muted("Builds the change a reviewer asked for, on its own branch."),
                    row![
                        pill(Tone::Busy, "Running"),
                        pill(Tone::Wait, "Needs OK"),
                        pill(Tone::Good, "Done")
                    ]
                    .spacing(8),
                    muted("Started by an event · 14:02 · 3 min ago"),
                ]
                .spacing(10),
            )
            .padding(16)
            .width(Length::Fill)
            .height(Length::Fill),
            Message::Hover,
        )
        .radius(RADIUS)
        .inset(1.0),
    )
    .style(styles::card(tokens))
    .width(Length::Fill)
    .height(300);

    // Tiles: small rounded surfaces.
    let tiles = row(BOTS.iter().take(4).map(|name| {
        container(
            organic::backdrop(
                hover,
                Surface::of(("tile", name)),
                tint,
                container(text(*name).size(12).color(tokens.text))
                    .padding(Padding::from([0, 8]))
                    .align_y(Alignment::Center)
                    .width(Length::Fill)
                    .height(Length::Fill),
                Message::Hover,
            )
            .radius(RADIUS_S)
            .inset(1.0),
        )
        .style(move |_| container::Style {
            background: Some(Background::Color(tokens.surface)),
            border: Border {
                color: tokens.border,
                width: 1.0,
                radius: RADIUS_S.into(),
            },
            ..container::Style::default()
        })
        .width(96)
        .height(26)
        .into()
    }))
    .spacing(8);

    // Rows that scroll: square surfaces, each named by what it shows.
    let rows = column(BOTS.iter().map(|name| {
        organic::backdrop(
            hover,
            Surface::of(("row", name)),
            tint,
            container(
                row![
                    container(strong(name)).width(110),
                    muted("Review the fix before its pull request opens"),
                    Space::new().width(Length::Fill),
                    muted("3 min"),
                ]
                .spacing(12)
                .align_y(Alignment::Center),
            )
            .padding(Padding::from([0, 14]))
            .align_y(Alignment::Center)
            .width(Length::Fill)
            .height(36),
            Message::Hover,
        )
        .into()
    }));
    let list = container(scrollable(rows).height(Length::Fill))
        .padding(1)
        .style(styles::card(tokens))
        .width(Length::Fill)
        .height(Length::Fill);

    let switch = |label: &'static str, message: Message| {
        button(text(label).size(13))
            .on_press(message)
            .style(styles::button_style(tokens, styles::Kind::Secondary))
    };
    let motion = if hover.motion().is_on() {
        "Motion: on"
    } else {
        "Motion: off"
    };
    container(
        column![
            row![
                switch("Theme", Message::SwitchTheme),
                switch(motion, Message::SwitchMotion)
            ]
            .spacing(8),
            row![column![card, tiles].spacing(16).width(420), list].spacing(16),
        ]
        .spacing(16),
    )
    .padding(20)
    .width(Length::Fill)
    .height(Length::Fill)
    .style(move |_: &Theme| container::Style {
        background: Some(Background::Color(tokens.page)),
        ..container::Style::default()
    })
    .into()
}
